package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// taskError is a failure with a message meant for the dashboard.
type taskError struct{ msg string }

func (e taskError) Error() string { return e.msg }

func userErr(format string, args ...any) error { return taskError{fmt.Sprintf(format, args...)} }

// handleTask runs one task and reports the outcome.
func (a *Agent) handleTask(ctx context.Context, t Task) {
	var result any
	var err error
	switch t.Type {
	case "DEPLOY":
		result, err = a.deploy(ctx, t)
	case "STOP":
		err = a.lifecycle(ctx, t, "stop")
	case "START":
		err = a.lifecycle(ctx, t, "start")
	case "RESTART":
		err = a.lifecycle(ctx, t, "restart")
	case "DELETE":
		err = a.remove(ctx, t.Deployment)
	case "LOGS":
		result, err = a.containerLogs(ctx, t.Logs)
	case "ANALYZE":
		result, err = a.analyzeGit(ctx, t.Analyze)
	default:
		err = userErr("this agent doesn't know how to do %q; update the agent", t.Type)
	}

	status, msg := "DONE", ""
	if err != nil {
		status, msg = "FAILED", err.Error()
		var te taskError
		if !errors.As(err, &te) {
			log.Printf("task %s (%s): %v", t.ID, t.Type, err)
		}
	}
	a.reportWithRetry(ctx, t.ID, status, msg, result)
}

func (a *Agent) reportWithRetry(ctx context.Context, taskID, status, msg string, result any) {
	for attempt := 0; attempt < 6; attempt++ {
		if err := a.api.ReportTask(ctx, taskID, status, "", msg, result); err == nil {
			return
		} else {
			log.Printf("report task %s: %v", taskID, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
}

type serviceResult struct {
	Name          string `json:"name"`
	ContainerID   string `json:"container_id"`
	State         string `json:"state"`
	Image         string `json:"image"`
	DetectedPorts []int  `json:"detected_ports"`
}

type deployResult struct {
	ImageDigest string          `json:"image_digest,omitempty"`
	GitCommit   string          `json:"git_commit,omitempty"`
	Services    []serviceResult `json:"services"`
}

func (a *Agent) deploy(ctx context.Context, t Task) (*deployResult, error) {
	d := t.Deployment
	logs := a.newLogShipper(d.ID, d.RevisionID)
	defer logs.Close()
	phase := func(p string) { a.api.ReportTask(ctx, t.ID, "RUNNING", p, "", nil) }

	logs.Deploy(fmt.Sprintf("Deploying %s (revision #%d) on %s", d.Name, d.Revision, a.info.Hostname))
	var res *deployResult
	var err error
	switch d.Type {
	case "IMAGE", "DOCKERFILE":
		res, err = a.deployContainer(ctx, d, logs, phase)
	case "COMPOSE":
		res, err = a.deployCompose(ctx, d, logs, phase)
	default:
		err = userErr("unknown deployment type %q", d.Type)
	}
	if err != nil {
		var te taskError
		if !errors.As(err, &te) {
			err = userErr("%v", err)
		}
		logs.Deploy("Error: " + err.Error())
		return nil, err
	}
	return res, nil
}

// --------------------------------------------------- image and Dockerfile

func (a *Agent) deployContainer(ctx context.Context, d *Deployment, logs *logShipper, phase func(string)) (*deployResult, error) {
	if len(d.Services) != 1 {
		return nil, userErr("expected exactly one service, got %d", len(d.Services))
	}
	svc := d.Services[0]
	res := &deployResult{}
	phase("BUILDING")

	image := d.Image
	if d.Type == "IMAGE" {
		logs.Deploy("Pulling image " + image + "...")
		err := a.docker.PullImage(ctx, image, d.RegistryAuths, logs.Deploy)
		if err != nil {
			return nil, userErr("Unable to pull image %s. Check that the image exists and the registry is accessible. (%v)", image, err)
		}
		logs.Deploy("Image pulled.")
	} else {
		image = d.BuildTag
		commit, err := a.buildFromSource(ctx, d, logs)
		if err != nil {
			return nil, err
		}
		res.GitCommit = commit
	}

	info, err := a.docker.InspectImage(ctx, image)
	if err != nil {
		return nil, userErr("Could not inspect image %s: %v", image, err)
	}
	res.ImageDigest = info.Digest()
	if svc.Public && svc.Port == 0 {
		return nil, userErr("Choose which port to make public.")
	}

	var volumes []VolumeMount
	for _, v := range d.Volumes {
		if strings.HasPrefix(v.Name, "/") && !a.hostPathAllowed(v.Name) {
			return nil, userErr("Host path %s can't be mounted on this machine. Use a named volume, or allow the folder with INSTA_DEPLOY_ALLOWED_HOST_PATHS on the agent.", v.Name)
		}
		volumes = append(volumes, VolumeMount{Source: v.Name, Target: v.Target, ReadOnly: v.ReadOnly})
	}
	env := make([]string, 0, len(svc.Env))
	for k, v := range svc.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	labels := map[string]string{labelManaged: "true", labelDeploymentID: d.ID, labelService: svc.Name}
	if hc := svc.HealthCheck; hc != nil && svc.Port > 0 {
		labels[labelHealthPath] = hc.Path
		labels[labelHealthPort] = strconv.Itoa(svc.Port)
		labels[labelHealthEvery] = strconv.Itoa(hc.IntervalSeconds)
	}

	phase("DEPLOYING")
	logs.Deploy("Starting container " + d.ContainerName + "...")
	// Replace the previous container (same name, so the public route keeps
	// pointing at the right place).
	if err := a.docker.RemoveContainer(ctx, d.ContainerName); err != nil {
		return nil, userErr("Could not remove the previous container: %v", err)
	}
	id, err := a.docker.CreateContainer(ctx, ContainerSpec{
		Name: d.ContainerName, Image: image, Env: env, Port: svc.Port, Network: d.Network, Labels: labels,
		Volumes: volumes, RestartPolicy: d.RestartPolicy, CPUs: d.CPUs, MemoryMB: d.MemoryMB,
	})
	if err != nil {
		return nil, userErr("Could not create the container: %v", err)
	}
	if d.ProjectNetwork != "" {
		// Reachable by the project's other deployments as svc.ProjectAliases.
		if err := a.docker.EnsureNetwork(ctx, d.ProjectNetwork); err != nil {
			return nil, userErr("Could not create the project network: %v", err)
		}
		if err := a.docker.ConnectNetworkAs(ctx, d.ProjectNetwork, id, svc.ProjectAliases); err != nil {
			return nil, userErr("Could not join the project network: %v", err)
		}
	}
	if err := a.docker.StartContainer(ctx, id); err != nil {
		return nil, userErr("The container could not be started: %v", err)
	}

	// Catch containers that exit right away (bad command, missing config).
	// The restart policy would otherwise hide this as a crash loop.
	time.Sleep(3 * time.Second)
	ci, err := a.docker.GetContainer(ctx, id)
	if err == nil && (ci.State.Status != "running" || ci.RestartCount > 0) {
		a.docker.StopContainer(ctx, id) // keep it for `docker logs`
		msg := fmt.Sprintf("The container exited right after starting (exit code %d). Check the container logs.", ci.State.ExitCode)
		if ci.State.Error != "" {
			msg += " " + ci.State.Error
		}
		return nil, userErr("%s", msg)
	}
	logs.Deploy("Container running.")
	if d.Type == "DOCKERFILE" {
		a.pruneBuiltImages(ctx, d)
	}
	res.Services = []serviceResult{{Name: svc.Name, ContainerID: id, State: "running", Image: image, DetectedPorts: info.ExposedPorts()}}
	return res, nil
}

// buildFromSource fetches the build context and builds d.BuildTag. On a
// rollback, the earlier revision's image is reused if it still exists.
func (a *Agent) buildFromSource(ctx context.Context, d *Deployment, logs *logShipper) (string, error) {
	if d.ReuseImage != "" {
		if _, err := a.docker.InspectImage(ctx, d.ReuseImage); err == nil {
			logs.Build("Reusing previously built image " + d.ReuseImage)
			return d.Source.GitCommit, a.docker.TagImage(ctx, d.ReuseImage, d.BuildTag)
		}
	}
	dir, err := os.MkdirTemp(a.workDir("builds"), "build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir) // build contexts are deleted after the build

	commit, err := a.fetchSource(ctx, d.Source, dir, logs)
	if err != nil {
		return "", err
	}
	dockerfile := d.Source.Path
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if p, err := safeJoin(dir, dockerfile); err != nil {
		return "", userErr("Invalid Dockerfile path %q", dockerfile)
	} else if _, err := os.Stat(p); err != nil {
		return "", userErr("No Dockerfile found at %s in the project.", dockerfile)
	}

	logs.Build("Building image...")
	if err := buildImage(ctx, dir, dockerfile, d.BuildTag, d.RegistryAuths, logs.Build); err != nil {
		return "", userErr("Docker build failed. View the build logs for details.")
	}
	logs.Build("Build complete.")
	return commit, nil
}

// fetchSource puts the project files into dir: a Git clone, or an inline
// Compose file. Returns the Git commit, if any.
func (a *Agent) fetchSource(ctx context.Context, src *Source, dir string, logs *logShipper) (string, error) {
	switch {
	case src == nil:
		return "", userErr("this deployment has no source files")
	case src.Kind == "upload":
		return "", userErr("ZIP uploads are no longer supported. Switch this deployment to a Git repository.")
	case src.Kind == "git":
		logs.Build(fmt.Sprintf("Cloning %s (%s)...", src.GitURL, src.GitRef))
		commit, err := gitClone(ctx, src.GitURL, src.GitRef, src.GitCommit, src.GitToken, dir, logs.Build)
		if err != nil {
			return "", userErr("Could not clone the repository: %v", err)
		}
		logs.Build("Checked out " + shortSHA(commit))
		return commit, nil
	case src.Kind == "inline":
		return "", os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(src.Compose), 0o600)
	}
	return "", userErr("unknown source %q", src.Kind)
}

// pruneBuiltImages keeps the 5 most recent builds of a deployment (for
// quick rollbacks) and removes older ones.
func (a *Agent) pruneBuiltImages(ctx context.Context, d *Deployment) {
	repo, _ := splitImage(d.BuildTag)
	tags, err := a.docker.ListImageTags(ctx, repo)
	if err != nil {
		return
	}
	revision := func(tag string) int {
		_, t := splitImage(tag)
		n, _ := strconv.Atoi(strings.TrimPrefix(t, "r"))
		return n
	}
	sort.Slice(tags, func(i, j int) bool { return revision(tags[i]) > revision(tags[j]) })
	for i, tag := range tags {
		if i >= 5 && tag != d.BuildTag {
			a.docker.RemoveImage(ctx, tag)
		}
	}
}

// ------------------------------------------------------------- Compose

func (a *Agent) deployCompose(ctx context.Context, d *Deployment, logs *logShipper, phase func(string)) (*deployResult, error) {
	phase("BUILDING")
	res := &deployResult{}
	projectDir := a.workDir("projects", d.ID)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return nil, err
	}

	// Fetch into a fresh folder, then sync it over the project folder: files
	// removed from the source since the last deploy are deleted, while data
	// written by containers into the project (relative bind mounts) survives.
	staging, err := os.MkdirTemp(a.workDir("builds"), "compose-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	if res.GitCommit, err = a.fetchSource(ctx, d.Source, staging, logs); err != nil {
		return nil, err
	}
	if err := syncTree(staging, projectDir); err != nil {
		return nil, err
	}

	file := d.Source.Path
	if d.Source.Kind == "inline" || file == "" {
		file = findComposeFile(projectDir)
	}
	if _, err := safeJoin(projectDir, file); err != nil || file == "" {
		return nil, userErr("No Compose file found. Add a compose.yaml to the project.")
	}
	if _, err := os.Stat(filepath.Join(projectDir, file)); err != nil {
		return nil, userErr("No Compose file found at %s.", file)
	}
	if !a.hostMountOK && usesRelativeBinds(projectDir, file) {
		return nil, userErr("This Compose file mounts files from the project, which requires the agent to be started with -v /var/lib/insta-deploy:/var/lib/insta-deploy. Re-run the agent with the command from the Agents page.")
	}

	if d.ProjectNetwork != "" {
		// Compose joins it as an external network (see transformCompose).
		if err := a.docker.EnsureNetwork(ctx, d.ProjectNetwork); err != nil {
			return nil, userErr("Could not create the project network: %v", err)
		}
	}

	logs.Deploy("Reading " + file + "...")
	// Project and deployment variables are available as ${VAR} in the file.
	project, err := normalizeCompose(ctx, projectDir, file, d.ContainerName, commonEnv(d))
	if err != nil {
		return nil, userErr("%v", err)
	}
	if problems := validateCompose(project, composePolicy{projectDir: projectDir, allowedHostPaths: a.allowedHostPaths}); len(problems) > 0 {
		return nil, userErr("The Compose file uses settings that aren't allowed:\n- %s", strings.Join(problems, "\n- "))
	}
	if err := transformCompose(project, d); err != nil {
		return nil, userErr("%v", err)
	}
	final := filepath.Join(projectDir, ".insta-deploy.compose.json")
	if err := writeComposeFile(project, final); err != nil {
		return nil, err
	}

	cfg, cleanup, err := dockerConfigDir(d.RegistryAuths)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	compose := func(stream func(string), args ...string) error {
		base := []string{"compose", "--progress", "plain", "--project-directory", projectDir, "-f", final, "-p", d.ContainerName}
		return command{name: "docker", args: append(base, args...), dir: projectDir, env: []string{"DOCKER_CONFIG=" + cfg}, onLine: stream}.run(ctx)
	}

	logs.Build("Pulling and building images...")
	if err := compose(withoutLayerProgress(logs.Build), "pull", "--ignore-buildable", "--ignore-pull-failures"); err != nil {
		logs.Build("Some images could not be pulled; trying to continue.")
	}
	if err := compose(logs.Build, "build"); err != nil {
		return nil, userErr("Docker build failed. View the build logs for details.")
	}

	phase("DEPLOYING")
	logs.Deploy("Starting services...")
	if err := compose(logs.Deploy, "up", "-d", "--remove-orphans", "--no-build"); err != nil {
		return nil, userErr("Docker Compose could not start the project: %s", lastLine(err.Error()))
	}

	time.Sleep(3 * time.Second)
	containers, err := a.docker.ListContainers(ctx, labelComposeProj+"="+d.ContainerName)
	if err != nil {
		return nil, err
	}
	normalized := project.services()
	var failed []string
	for _, c := range containers {
		name := c.Labels[labelComposeSvc]
		ci, err := a.docker.GetContainer(ctx, c.ID)
		state := c.State
		if err == nil {
			state = ci.State.Status
		}
		ports := composePorts(normalized[name])
		if info, err := a.docker.InspectImage(ctx, c.Image); err == nil && len(ports) == 0 {
			ports = info.ExposedPorts()
		}
		res.Services = append(res.Services, serviceResult{Name: name, ContainerID: c.ID, State: state, Image: c.Image, DetectedPorts: ports})
		if isPublic(d, name) && state != "running" {
			failed = append(failed, name)
		}
		logs.Deploy(fmt.Sprintf("  %s: %s", name, state))
	}
	if len(failed) > 0 {
		return nil, userErr("Public service(s) %s are not running. Check the container logs.", strings.Join(failed, ", "))
	}
	logs.Deploy("All services started.")
	return res, nil
}

func findComposeFile(dir string) string {
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return name
		}
	}
	return ""
}

// usesRelativeBinds is a quick check for "./something:/path" mounts, which
// need the project folder to exist at the same path on the host.
func usesRelativeBinds(dir, file string) bool {
	b, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "- ./") || strings.Contains(s, "- ../") || strings.Contains(s, "source: ./") || strings.Contains(s, "file: ./")
}

func commonEnv(d *Deployment) map[string]string {
	// Variables shared by all services (project/deployment level) are the
	// ones present in every service's env.
	if len(d.Services) == 0 {
		return nil
	}
	common := map[string]string{}
	for k, v := range d.Services[0].Env {
		common[k] = v
	}
	for _, s := range d.Services[1:] {
		for k, v := range common {
			if s.Env[k] != v {
				delete(common, k)
			}
		}
	}
	return common
}

func isPublic(d *Deployment, name string) bool {
	for _, s := range d.Services {
		if s.Name == name {
			return s.Public
		}
	}
	return false
}

// --------------------------------------------------- stop / start / delete

// containersOf lists a deployment's containers: one for IMAGE/DOCKERFILE,
// one per service for COMPOSE.
func (a *Agent) containersOf(ctx context.Context, d *Deployment) ([]ContainerSummary, error) {
	if d.Type == "COMPOSE" {
		return a.docker.ListContainers(ctx, labelComposeProj+"="+d.ContainerName)
	}
	all, err := a.docker.ListContainers(ctx, labelDeploymentID+"="+d.ID)
	if err != nil {
		return nil, err
	}
	return all, nil
}

func (a *Agent) lifecycle(ctx context.Context, t Task, action string) error {
	d := t.Deployment
	containers, err := a.containersOf(ctx, d)
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		if action == "stop" {
			return nil
		}
		return userErr("No containers found for %s on this machine. Redeploy it instead.", d.Name)
	}
	if action != "stop" {
		a.api.ReportTask(ctx, t.ID, "RUNNING", "DEPLOYING", "", nil)
	}
	for _, c := range containers {
		var err error
		switch action {
		case "stop":
			err = a.docker.StopContainer(ctx, c.ID)
		case "start":
			if c.State != "running" {
				err = a.docker.StartContainer(ctx, c.ID)
			}
		case "restart":
			err = a.docker.RestartContainer(ctx, c.ID)
		}
		if err != nil {
			return userErr("Could not %s %s: %v", action, strings.TrimPrefix(firstName(c), "/"), err)
		}
	}
	return nil
}

func firstName(c ContainerSummary) string {
	if len(c.Names) > 0 {
		return c.Names[0]
	}
	return c.ID[:12]
}

// remove deletes everything a deployment created on this machine.
func (a *Agent) remove(ctx context.Context, d *Deployment) error {
	containers, err := a.containersOf(ctx, d)
	if err != nil {
		return err
	}
	for _, c := range containers {
		if err := a.docker.RemoveContainer(ctx, c.ID); err != nil {
			return userErr("Could not remove container %s: %v", firstName(c), err)
		}
	}
	switch d.Type {
	case "COMPOSE":
		a.docker.RemoveNetworks(ctx, labelComposeProj+"="+d.ContainerName)
		if d.DeleteVolumes {
			if err := a.docker.RemoveVolumes(ctx, labelComposeProj+"="+d.ContainerName); err != nil {
				return userErr("Containers removed, but volumes could not be deleted: %v", err)
			}
		}
		// Images Compose built for this project are named "<project>-<service>".
		if tags, err := a.docker.ListImageTags(ctx, d.ContainerName+"-*"); err == nil {
			for _, tag := range tags {
				a.docker.RemoveImage(ctx, tag)
			}
		}
		os.RemoveAll(a.workDir("projects", d.ID))
	default:
		if d.DeleteVolumes {
			for _, v := range d.Volumes {
				if err := a.docker.RemoveVolume(ctx, v.Name); err != nil {
					return userErr("Container removed, but volume %s could not be deleted: %v", v.Name, err)
				}
			}
		}
		if d.Type == "DOCKERFILE" {
			repo := "insta-deploy/" + strings.ReplaceAll(d.ID, "-", "")[:8]
			if tags, err := a.docker.ListImageTags(ctx, repo); err == nil {
				for _, tag := range tags {
					a.docker.RemoveImage(ctx, tag)
				}
			}
		}
	}
	if d.ProjectNetwork != "" {
		// Docker refuses while other deployments of the project still use it.
		a.docker.RemoveNetwork(ctx, d.ProjectNetwork)
	}
	return nil
}

// ------------------------------------------------------------ logs

func (a *Agent) containerLogs(ctx context.Context, req *LogsRequest) (map[string]string, error) {
	target := req.ContainerName
	if req.Compose {
		cs, err := a.docker.ListContainers(ctx, labelComposeProj+"="+req.ContainerName, labelComposeSvc+"="+req.Service)
		if err != nil {
			return nil, err
		}
		if len(cs) == 0 {
			return nil, userErr("Service %s has no container on this machine.", req.Service)
		}
		target = cs[0].ID
	}
	out, err := a.docker.GetLogs(ctx, target, req.Tail)
	if errors.Is(err, errNotFound) {
		return nil, userErr("The container doesn't exist on this machine. Redeploy to recreate it.")
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"lines": out}, nil
}

// ------------------------------------------------------------ analyze

type dockerfileInfo struct {
	Path  string `json:"path"`
	Ports []int  `json:"ports"`
}

type composeServiceInfo struct {
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	Build       string   `json:"build,omitempty"`
	Ports       []int    `json:"ports"`
	Environment []string `json:"environment"`
	Volumes     []string `json:"volumes"`
	DependsOn   []string `json:"depends_on"`
	Healthcheck bool     `json:"healthcheck"`
}

// analyzeGit clones a repository and reports its Dockerfiles and Compose
// services for the deploy form.
func (a *Agent) analyzeGit(ctx context.Context, in *AnalyzeInput) (map[string]any, error) {
	dir, err := os.MkdirTemp(a.workDir("builds"), "analyze-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if _, err := gitClone(ctx, in.GitURL, in.GitRef, in.GitCommit, in.GitToken, dir, nil); err != nil {
		return nil, userErr("Could not clone the repository: %v", err)
	}

	dockerfiles := []dockerfileInfo{}
	composeFiles := []string{}
	filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() && (e.Name() == "node_modules" || e.Name() == ".git" || strings.Count(p[len(dir):], "/") > 4) {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(dir, p)
		name := e.Name()
		switch {
		case name == "Dockerfile" || strings.HasPrefix(name, "Dockerfile.") || strings.HasSuffix(name, ".Dockerfile"):
			b, _ := os.ReadFile(p)
			dockerfiles = append(dockerfiles, dockerfileInfo{Path: rel, Ports: dockerfileExposes(string(b))})
		case name == "compose.yaml" || name == "compose.yml" || name == "docker-compose.yaml" || name == "docker-compose.yml":
			composeFiles = append(composeFiles, rel)
		}
		return nil
	})
	sort.Slice(composeFiles, func(i, j int) bool { return strings.Count(composeFiles[i], "/") < strings.Count(composeFiles[j], "/") })
	sort.Slice(dockerfiles, func(i, j int) bool {
		return strings.Count(dockerfiles[i].Path, "/") < strings.Count(dockerfiles[j].Path, "/")
	})

	out := map[string]any{"root": "", "files": 0, "dockerfiles": dockerfiles, "compose_files": composeFiles}
	if len(composeFiles) > 0 {
		file := composeFiles[0]
		project, err := normalizeCompose(ctx, dir, file, "insta-analyze", nil)
		if err != nil {
			out["compose_error"] = err.Error()
			return out, nil
		}
		services := []composeServiceInfo{}
		for _, name := range project.serviceNames() {
			svc := project.services()[name]
			info := composeServiceInfo{Name: name, Ports: composePorts(svc), Environment: []string{}, Volumes: []string{}, DependsOn: []string{}}
			info.Image, _ = svc["image"].(string)
			if b, ok := svc["build"].(map[string]any); ok {
				info.Build, _ = b["context"].(string)
				info.Build, _ = filepath.Rel(dir, info.Build)
			}
			if env, ok := svc["environment"].(map[string]any); ok {
				for k := range env {
					info.Environment = append(info.Environment, k)
				}
				sort.Strings(info.Environment)
			}
			for _, v := range asMaps(svc["volumes"]) {
				src, _ := v["source"].(string)
				dst, _ := v["target"].(string)
				info.Volumes = append(info.Volumes, strings.TrimPrefix(src, dir+"/")+":"+dst)
			}
			if deps, ok := svc["depends_on"].(map[string]any); ok {
				for k := range deps {
					info.DependsOn = append(info.DependsOn, k)
				}
				sort.Strings(info.DependsOn)
			}
			_, info.Healthcheck = svc["healthcheck"]
			services = append(services, info)
		}
		warnings := validateCompose(project, composePolicy{projectDir: dir, allowedHostPaths: a.allowedHostPaths})
		if warnings == nil {
			warnings = []string{}
		}
		out["compose_path"] = file
		out["compose"] = map[string]any{"services": services, "warnings": warnings}
	}
	return out, nil
}

// dockerfileExposes reads EXPOSE instructions from a Dockerfile.
func dockerfileExposes(content string) []int {
	set := map[int]bool{}
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "EXPOSE") {
			continue
		}
		for _, f := range fields[1:] {
			port, proto, _ := strings.Cut(f, "/")
			if n, err := strconv.Atoi(port); err == nil && n > 0 && n < 65536 && (proto == "" || proto == "tcp") {
				set[n] = true
			}
		}
	}
	ports := []int{}
	for n := range set {
		ports = append(ports, n)
	}
	sort.Ints(ports)
	return ports
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// layerLine matches Docker's per-layer pull progress ("a4e5eae782f7
// Downloading [===>   ] 26MB/51MB", "Pull complete", "Waiting", ...).
var layerLine = regexp.MustCompile(`^\s*[0-9a-f]{12}\s`)

// withoutLayerProgress drops per-layer pull progress, which repeats for
// every update of every layer, and keeps the per-service lines
// ("web Pulling", "web Pulled").
func withoutLayerProgress(stream func(string)) func(string) {
	return func(line string) {
		if !layerLine.MatchString(line) {
			stream(line)
		}
	}
}
