package main

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Converting `docker run ...` into a Compose service means a pasted
// command gets the same validation as any Compose deployment, and keeps
// its command, volumes, restart policy and so on. Nothing is executed: the
// command is only split into words like a shell would.

type composeServiceOut struct {
	Image       string            `yaml:"image"`
	Entrypoint  []string          `yaml:"entrypoint,omitempty"`
	Command     []string          `yaml:"command,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	Expose      []string          `yaml:"expose,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty"`
	Restart     string            `yaml:"restart,omitempty"`
	User        string            `yaml:"user,omitempty"`
	WorkingDir  string            `yaml:"working_dir,omitempty"`
	Hostname    string            `yaml:"hostname,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	ExtraHosts  []string          `yaml:"extra_hosts,omitempty"`
	DNS         []string          `yaml:"dns,omitempty"`
	Tmpfs       []string          `yaml:"tmpfs,omitempty"`
	CPUs        string            `yaml:"cpus,omitempty"`
	MemLimit    string            `yaml:"mem_limit,omitempty"`
	ShmSize     string            `yaml:"shm_size,omitempty"`
	Init        bool              `yaml:"init,omitempty"`
	ReadOnly    bool              `yaml:"read_only,omitempty"`
	Platform    string            `yaml:"platform,omitempty"`
	StopSignal  string            `yaml:"stop_signal,omitempty"`
}

type DockerRunResult struct {
	Name      string          `json:"name"`
	Service   string          `json:"service"`
	Compose   string          `json:"compose"`
	Variables []variableInput `json:"variables"`
	Ports     []int           `json:"ports"`
	Warnings  []string        `json:"warnings"`
}

// shellSplit splits a command line into words: single and double quotes,
// backslash escapes, and backslash-newline continuations.
func shellSplit(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && (s[i+1] == '\n' || s[i+1] == '\r'):
			i++
			if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, errors.New("unclosed ' quote")
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
			inWord = true
		case c == '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.ContainsRune(`"\$`+"`", rune(s[i+1])) {
					i++
				}
				cur.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, errors.New(`unclosed " quote`)
			}
			inWord = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// Flags that take a value (long and short names).
var runValueFlags = map[string]bool{
	"-e": true, "--env": true, "--env-file": true, "-p": true, "--publish": true, "--expose": true,
	"-v": true, "--volume": true, "--mount": true, "--name": true, "--restart": true, "-w": true, "--workdir": true,
	"-u": true, "--user": true, "--entrypoint": true, "-h": true, "--hostname": true, "-l": true, "--label": true,
	"--add-host": true, "--dns": true, "--tmpfs": true, "--cpus": true, "-m": true, "--memory": true,
	"--shm-size": true, "--platform": true, "--stop-signal": true, "--network": true, "--net": true,
	"--cap-add": true, "--cap-drop": true, "--device": true, "--gpus": true, "--pid": true, "--ipc": true,
	"--security-opt": true, "--log-driver": true, "--log-opt": true, "--pull": true, "--health-cmd": true,
	"--health-interval": true, "--health-retries": true, "--health-timeout": true, "--health-start-period": true,
	"--ulimit": true, "--sysctl": true, "--memory-swap": true, "--memory-reservation": true, "--cpu-shares": true,
	"--network-alias": true, "--userns": true, "--uts": true, "--cgroupns": true, "--stop-timeout": true,
	"--label-file": true, "--group-add": true, "--volumes-from": true, "--link": true, "-c": true,
}

// Boolean flags (single letters can be combined, e.g. -dit).
var runBoolFlags = map[string]bool{
	"-d": true, "--detach": true, "-i": true, "--interactive": true, "-t": true, "--tty": true, "--rm": true,
	"--privileged": true, "--init": true, "--read-only": true, "-P": true, "--publish-all": true,
	"--no-healthcheck": true, "-q": true, "--quiet": true,
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func convertDockerRun(command string) (*DockerRunResult, error) {
	words, err := shellSplit(command)
	if err != nil {
		return nil, fmt.Errorf("couldn't read the command: %v", err)
	}
	// Accept "docker run", "docker container run", "sudo docker run", or just the flags.
	for len(words) > 0 && (words[0] == "sudo" || words[0] == "docker" || words[0] == "podman" || words[0] == "container" || words[0] == "run" || words[0] == "$") {
		words = words[1:]
	}
	if len(words) == 0 {
		return nil, errors.New("paste a docker run command, like: docker run -d -p 8080:80 nginx")
	}

	svc := composeServiceOut{Environment: map[string]string{}, Labels: map[string]string{}}
	res := &DockerRunResult{Variables: []variableInput{}, Ports: []int{}, Warnings: []string{}}
	warn := func(format string, args ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(format, args...)) }
	namedVolumes := map[string]bool{}
	ports := map[int]bool{}

	i := 0
	next := func(flag string) (string, error) {
		i++
		if i >= len(words) {
			return "", fmt.Errorf("%s needs a value", flag)
		}
		return words[i], nil
	}
	for ; i < len(words); i++ {
		w := words[i]
		if !strings.HasPrefix(w, "-") || w == "-" {
			break // the image
		}
		if w == "--" {
			i++
			break
		}
		flag, value, hasValue := strings.Cut(w, "=")
		if !hasValue && runValueFlags[flag] {
			if value, err = next(flag); err != nil {
				return nil, err
			}
		} else if !hasValue && !runBoolFlags[flag] && !strings.HasPrefix(flag, "--") && len(flag) > 2 {
			// Combined short flags like -dit, or -p8080:80.
			if runValueFlags[flag[:2]] {
				flag, value = flag[:2], flag[2:]
			} else {
				for _, c := range flag[1:] {
					if !runBoolFlags["-"+string(c)] {
						warn("Ignored unknown option -%c", c)
					}
				}
				continue
			}
		}

		switch flag {
		case "-d", "--detach", "-i", "--interactive", "-t", "--tty", "--rm", "--pull", "-q", "--quiet":
			// Not meaningful for a managed deployment.
		case "-e", "--env":
			k, v, ok := strings.Cut(value, "=")
			if !envNameRe.MatchString(k) {
				warn("Skipped environment variable %q: invalid name", k)
				continue
			}
			if !ok {
				warn("%s has no value (it would come from your shell); set it under Environment variables", k)
				continue
			}
			res.Variables = append(res.Variables, variableInput{Key: k, Value: v, Secret: looksSecret(k)})
		case "--env-file", "--label-file":
			warn("%s %s can't be read; add those variables under Environment variables instead", flag, value)
		case "-p", "--publish":
			port := publishedPort(value)
			if port == 0 {
				warn("Couldn't read port %q", value)
				continue
			}
			ports[port] = true
		case "--expose":
			if n, err := strconv.Atoi(strings.Split(value, "/")[0]); err == nil {
				ports[n] = true
			}
		case "-P", "--publish-all":
			warn("--publish-all is ignored; choose which port to make public")
		case "-v", "--volume":
			vol, named, err := convertVolume(value)
			if err != nil {
				warn("%v", err)
				continue
			}
			if named != "" {
				namedVolumes[named] = true
			}
			if strings.HasPrefix(vol, "/") {
				warn("Host folder %s can only be mounted if the machine allows it (INSTA_DEPLOY_ALLOWED_HOST_PATHS on the agent); otherwise use a named volume", strings.Split(vol, ":")[0])
			}
			svc.Volumes = append(svc.Volumes, vol)
		case "--mount":
			vol, named, err := convertMount(value)
			if err != nil {
				warn("%v", err)
				continue
			}
			if named != "" {
				namedVolumes[named] = true
			}
			svc.Volumes = append(svc.Volumes, vol)
		case "--name":
			res.Name = slugName(value)
		case "--restart":
			svc.Restart = value
		case "-w", "--workdir":
			svc.WorkingDir = value
		case "-u", "--user":
			svc.User = value
		case "--entrypoint":
			svc.Entrypoint = []string{value}
		case "-h", "--hostname":
			svc.Hostname = value
		case "-l", "--label":
			k, v, _ := strings.Cut(value, "=")
			svc.Labels[k] = v
		case "--add-host":
			svc.ExtraHosts = append(svc.ExtraHosts, value)
		case "--dns":
			svc.DNS = append(svc.DNS, value)
		case "--tmpfs":
			svc.Tmpfs = append(svc.Tmpfs, value)
		case "--cpus":
			svc.CPUs = value
		case "-m", "--memory":
			svc.MemLimit = value
		case "--shm-size":
			svc.ShmSize = value
		case "--init":
			svc.Init = true
		case "--read-only":
			svc.ReadOnly = true
		case "--platform":
			svc.Platform = value
		case "--stop-signal":
			svc.StopSignal = value
		case "--network", "--net":
			if value == "host" {
				warn("--network host isn't allowed; the app is reached through its public URL instead")
			} else if value != "bridge" && value != "default" {
				warn("--network %s is ignored; the app gets its own network", value)
			}
		case "--privileged":
			warn("--privileged isn't allowed and was removed")
		case "--cap-add", "--device", "--gpus", "--pid", "--ipc", "--security-opt", "--userns", "--uts", "--cgroupns", "--volumes-from", "--link":
			warn("%s %s isn't supported and was removed", flag, value)
		default:
			warn("Ignored option %s", flag)
		}
	}
	if i >= len(words) {
		return nil, errors.New("no image found in the command")
	}
	svc.Image = words[i]
	if !validImage(svc.Image) {
		return nil, fmt.Errorf("%q is not a valid image name", svc.Image)
	}
	if rest := words[i+1:]; len(rest) > 0 {
		svc.Command = rest
	}
	if svc.Restart == "" {
		svc.Restart = "unless-stopped"
	}
	if len(svc.Environment) == 0 {
		svc.Environment = nil
	}
	if len(svc.Labels) == 0 {
		svc.Labels = nil
	}

	// Variables go into the deployment (so they can be secrets) and are
	// referenced from the Compose file.
	if len(res.Variables) > 0 {
		svc.Environment = map[string]string{}
		for _, v := range res.Variables {
			svc.Environment[v.Key] = "${" + v.Key + "}"
		}
	}
	for p := range ports {
		res.Ports = append(res.Ports, p)
		svc.Expose = append(svc.Expose, strconv.Itoa(p))
	}
	sort.Ints(res.Ports)
	sort.Strings(svc.Expose)

	res.Service = slugName(path.Base(imageRepository(svc.Image)))
	if res.Service == "" {
		res.Service = "app"
	}
	if res.Name == "" {
		res.Name = res.Service
	}
	doc := map[string]any{"services": map[string]any{res.Service: svc}}
	if len(namedVolumes) > 0 {
		vols := map[string]any{}
		for v := range namedVolumes {
			vols[v] = map[string]any{}
		}
		doc["volumes"] = vols
	}
	var out strings.Builder
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	res.Compose = out.String()
	return res, nil
}

// publishedPort returns the container port of a -p value:
// 8080:80, 127.0.0.1:8080:80, 80, 8080:80/tcp.
func publishedPort(v string) int {
	v, proto, _ := strings.Cut(v, "/")
	if proto != "" && proto != "tcp" {
		return 0
	}
	parts := strings.Split(v, ":")
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || !validPort(n) {
		return 0
	}
	return n
}

// convertVolume turns -v src:dst[:opts] into a Compose volume entry. Named
// volumes are returned so they can be declared.
func convertVolume(v string) (entry, named string, err error) {
	parts := strings.Split(v, ":")
	switch len(parts) {
	case 1:
		return v, "", nil // anonymous volume
	case 2, 3:
		src := parts[0]
		if !strings.HasPrefix(src, "/") && !strings.HasPrefix(src, ".") && !strings.HasPrefix(src, "~") {
			if !volumeNameRe.MatchString(src) {
				return "", "", fmt.Errorf("invalid volume name %q", src)
			}
			named = src
		}
		if strings.HasPrefix(src, "~") || strings.Contains(src, "$") {
			return "", "", fmt.Errorf("volume %q uses a path from your shell; use a named volume instead", v)
		}
		return v, named, nil
	}
	return "", "", fmt.Errorf("couldn't read volume %q", v)
}

func convertMount(v string) (entry, named string, err error) {
	opts := map[string]string{}
	for _, kv := range strings.Split(v, ",") {
		k, val, _ := strings.Cut(kv, "=")
		opts[k] = val
	}
	src := opts["source"]
	if src == "" {
		src = opts["src"]
	}
	dst := opts["target"]
	if dst == "" {
		dst = opts["destination"]
	}
	if dst == "" {
		dst = opts["dst"]
	}
	if dst == "" {
		return "", "", fmt.Errorf("--mount %q has no target", v)
	}
	if opts["type"] == "tmpfs" {
		return "", "", fmt.Errorf("--mount type=tmpfs: use --tmpfs %s instead", dst)
	}
	entry = dst
	if src != "" {
		entry = src + ":" + dst
		if opts["type"] != "bind" {
			named = src
		}
	}
	if _, ro := opts["readonly"]; ro {
		entry += ":ro"
	}
	return entry, named, nil
}

var nonNameRe = regexp.MustCompile(`[^a-z0-9-]+`)

func slugName(s string) string {
	s = strings.Trim(nonNameRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 32 {
		s = strings.TrimRight(s[:32], "-")
	}
	return s
}

func looksSecret(key string) bool {
	k := strings.ToUpper(key)
	for _, w := range []string{"PASSWORD", "PASS", "SECRET", "TOKEN", "KEY", "PRIVATE", "CREDENTIAL"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

// POST /api/convert/docker-run {"command": "docker run ..."}
func (s *Server) handleConvertDockerRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Command) > 20000 {
		writeError(w, http.StatusBadRequest, "the command is too long")
		return
	}
	res, err := convertDockerRun(req.Command)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
