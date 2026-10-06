// The Insta Deploy agent runs on the user's machine next to Docker. It
// connects outbound to the Insta Deploy backend, waits for work, and runs
// containers through the local Docker Engine API (and the docker CLI for
// builds and Compose).
package main

import (
	"context"
	"errors"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	agentVersion      = "0.2.0"
	networkName       = "insta-deploy"
	newtContainerName = "insta-deploy-newt"
	heartbeatInterval = 10 * time.Second
	registerRetry     = time.Minute
)

var dockerSocket = envOr("DOCKER_SOCKET", "/var/run/docker.sock")

type Agent struct {
	api       *API
	docker    *DockerService
	health    *healthChecker
	newtImage string
	info      SystemInfo

	// Where projects and build contexts live. In a container this should
	// be bind-mounted at the same path on the host (hostMountOK), so files
	// from a project can be mounted into its containers.
	dataDir          string
	hostMountOK      bool
	allowedHostPaths []string

	inflight sync.Map // task ID -> struct{}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[agent] ")

	token := os.Getenv("INSTA_DEPLOY_TOKEN")
	server := strings.TrimRight(os.Getenv("INSTA_DEPLOY_SERVER"), "/")
	if token == "" || server == "" {
		log.Fatal("INSTA_DEPLOY_TOKEN and INSTA_DEPLOY_SERVER must be set")
	}
	u, err := url.Parse(server)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		log.Fatalf("INSTA_DEPLOY_SERVER %q is not a valid URL", server)
	}
	// The agent token gives control over this machine's Docker, so don't
	// send it in clear text unless explicitly allowed (local development).
	if u.Scheme == "http" && os.Getenv("INSTA_DEPLOY_ALLOW_HTTP") != "true" {
		log.Fatal("INSTA_DEPLOY_SERVER must use https:// (set INSTA_DEPLOY_ALLOW_HTTP=true for local development only)")
	}

	docker := NewDockerService(dockerSocket)
	a := &Agent{
		api:       NewAPI(server, token),
		docker:    docker,
		health:    newHealthChecker(docker),
		newtImage: envOr("NEWT_IMAGE", "fosrl/newt:latest"),
		dataDir:   envOr("INSTA_DEPLOY_DATA_DIR", "/var/lib/insta-deploy"),
	}
	for _, p := range strings.Split(os.Getenv("INSTA_DEPLOY_ALLOWED_HOST_PATHS"), ",") {
		if p = strings.TrimSpace(p); filepath.IsAbs(p) {
			a.allowedHostPaths = append(a.allowedHostPaths, filepath.Clean(p))
		}
	}
	// The token is only needed in memory. Removing it from the environment
	// means no child process (docker, git) can ever inherit it.
	os.Unsetenv("INSTA_DEPLOY_TOKEN")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := docker.Ping(ctx); err != nil {
		log.Fatalf("cannot talk to Docker: %v", err)
	}
	if a.info, err = docker.SystemInfo(ctx); err != nil {
		log.Fatalf("read Docker info: %v", err)
	}
	if err := docker.EnsureNetwork(ctx, networkName); err != nil {
		log.Fatalf("create docker network: %v", err)
	}
	for _, dir := range []string{"projects", "builds"} {
		if err := os.MkdirAll(a.workDir(dir), 0o755); err != nil {
			log.Fatalf("create %s: %v", a.workDir(dir), err)
		}
	}
	a.checkSelf(ctx)
	log.Printf("Insta Deploy agent %s on %s (%s, %s, Docker %s)", agentVersion, a.info.Hostname, a.info.OS, a.info.Arch, a.info.DockerVersion)

	go a.health.run(ctx)
	go a.commandLoop(ctx)
	a.run(ctx)
	log.Println("shutting down")
}

// checkSelf looks at the agent's own container (if it runs in one): it
// joins the shared network (for health checks), and checks whether the data
// folder is mounted from the same path on the host.
func (a *Agent) checkSelf(ctx context.Context) {
	hostname, _ := os.Hostname()
	self, err := a.docker.GetContainer(ctx, hostname)
	if err != nil {
		a.hostMountOK = true // not in a container: paths are host paths
		return
	}
	for _, m := range self.Mounts {
		if filepath.Clean(m.Destination) == filepath.Clean(a.dataDir) && filepath.Clean(m.Source) == filepath.Clean(a.dataDir) {
			a.hostMountOK = true
		}
	}
	if !a.hostMountOK {
		log.Printf("note: %s is not mounted from the host; Compose files that mount project files won't work. "+
			"Add -v %s:%s to the agent's docker run command.", a.dataDir, a.dataDir, a.dataDir)
	}
	if err := a.docker.ConnectNetwork(ctx, networkName, self.ID); err != nil {
		log.Printf("join %s network: %v (HTTP health checks may not work)", networkName, err)
	}
}

func (a *Agent) workDir(parts ...string) string {
	return filepath.Join(append([]string{a.dataDir}, parts...)...)
}

func (a *Agent) hostPathAllowed(p string) bool {
	for _, allowed := range a.allowedHostPaths {
		if isWithin(allowed, p) {
			return true
		}
	}
	return false
}

// run registers, then sends heartbeats until shutdown.
func (a *Agent) run(ctx context.Context) {
	tick := time.NewTicker(heartbeatInterval)
	defer tick.Stop()

	var tunnelReady bool
	var lastRegister time.Time
	register := func() {
		lastRegister = time.Now()
		res, err := a.api.Register(ctx, a.info)
		if err != nil {
			log.Printf("register: %v", err)
			return
		}
		log.Printf("registered as %q", res.Name)
		if res.Tunnel == nil {
			log.Printf("no tunnel yet: %s (will retry)", res.TunnelError)
			return
		}
		if err := a.ensureNewt(ctx, res.Tunnel); err != nil {
			log.Printf("start tunnel: %v", err)
			return
		}
		tunnelReady = true
	}

	register()
	a.heartbeat(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			a.heartbeat(ctx)
			if !tunnelReady && time.Since(lastRegister) > registerRetry {
				register()
			}
		}
	}
}

// commandLoop long-polls the backend for tasks and runs each one in its own
// goroutine. The backend only hands out one task per deployment at a time.
func (a *Agent) commandLoop(ctx context.Context) {
	for ctx.Err() == nil {
		tasks, err := a.api.Commands(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("poll commands: %v", err)
			wait := 5 * time.Second
			if errors.Is(err, errUnauthorized) {
				wait = time.Minute
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			continue
		}
		for _, t := range tasks {
			if _, busy := a.inflight.LoadOrStore(t.ID, struct{}{}); busy {
				continue
			}
			go func() {
				defer a.inflight.Delete(t.ID)
				if t.Deployment != nil {
					log.Printf("%s %s", strings.ToLower(t.Type), t.Deployment.Name)
				}
				a.handleTask(ctx, t)
			}()
		}
	}
}

// heartbeat reports the state, health and resource usage of every
// container Insta Deploy manages.
func (a *Agent) heartbeat(ctx context.Context) {
	containers, err := a.docker.ListContainers(ctx, labelManaged+"=true")
	if err != nil {
		log.Printf("list containers: %v", err)
	}
	reports := make([]ContainerReport, 0, len(containers))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, c := range containers {
		id := c.Labels[labelDeploymentID]
		if id == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := ContainerReport{DeploymentID: id, Service: c.Labels[labelService], ContainerID: c.ID, State: c.State, Health: "NONE"}
			if r.Service == "" {
				r.Service = c.Labels[labelComposeSvc]
			}
			if info, err := a.docker.GetContainer(ctx, c.ID); err == nil {
				r.State = info.State.Status
				if info.State.Health != nil {
					r.Health = strings.ToUpper(info.State.Health.Status)
				}
			}
			if r.Health == "NONE" {
				if h := a.health.result(c.ID); h != "" {
					r.Health = h
				} else if c.Labels[labelHealthPath] != "" {
					r.Health = "STARTING"
				}
			}
			if r.State == "running" {
				sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				if st, err := a.docker.GetStats(sctx, c.ID); err == nil {
					r.Stats = &st
				}
				cancel()
			}
			mu.Lock()
			reports = append(reports, r)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if err := a.api.Heartbeat(ctx, a.info, reports); err != nil {
		log.Printf("heartbeat: %v", err)
	}
}

// ensureNewt runs Pangolin's tunnel client (newt) as a container on the
// shared network. Pangolin then forwards public traffic through it to
// public containers by name.
func (a *Agent) ensureNewt(ctx context.Context, t *Tunnel) error {
	info, err := a.docker.GetContainer(ctx, newtContainerName)
	if err == nil && info.State.Status == "running" && info.Config.Labels[labelNewtID] == t.NewtID {
		return nil
	}
	if err != nil && !errors.Is(err, errNotFound) {
		return err
	}

	log.Printf("starting Pangolin tunnel (%s)", a.newtImage)
	if err := a.docker.PullImage(ctx, a.newtImage, nil, nil); err != nil {
		return err
	}
	if err := a.docker.RemoveContainer(ctx, newtContainerName); err != nil {
		return err
	}
	id, err := a.docker.CreateContainer(ctx, ContainerSpec{
		Name:  newtContainerName,
		Image: a.newtImage,
		Env: []string{
			"PANGOLIN_ENDPOINT=" + t.Endpoint,
			"NEWT_ID=" + t.NewtID,
			"NEWT_SECRET=" + t.NewtSecret,
		},
		Network: networkName,
		Labels:  map[string]string{labelNewtID: t.NewtID},
	})
	if err != nil {
		return err
	}
	return a.docker.StartContainer(ctx, id)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
