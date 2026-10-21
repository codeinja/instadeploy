package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// DockerService is a small client for the Docker Engine API over the local
// unix socket. It covers what the agent needs and nothing more; builds and
// Compose go through the docker CLI instead (see cli.go).
//
// API reference: https://docs.docker.com/reference/api/engine/
type DockerService struct {
	http *http.Client
}

const (
	labelManaged      = "insta-deploy.managed"
	labelDeploymentID = "insta-deploy.deployment-id"
	labelService      = "insta-deploy.service"
	labelHealthPath   = "insta-deploy.health.path"
	labelHealthPort   = "insta-deploy.health.port"
	labelHealthEvery  = "insta-deploy.health.interval"
	labelNewtID       = "insta-deploy.newt-id"
	labelComposeProj  = "com.docker.compose.project"
	labelComposeSvc   = "com.docker.compose.service"
)

var errNotFound = errors.New("not found")

// notFoundError keeps Docker's message but still matches errNotFound.
type notFoundError struct{ msg string }

func (e notFoundError) Error() string        { return e.msg }
func (e notFoundError) Is(target error) bool { return target == errNotFound }

func NewDockerService(socket string) *DockerService {
	return &DockerService{http: &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		},
	}}
}

func (d *DockerService) do(ctx context.Context, method, path string, body any, header http.Header) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, r)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach Docker (is /var/run/docker.sock mounted?): %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		var e struct {
			Message string `json:"message"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if json.Unmarshal(raw, &e) != nil || e.Message == "" {
			e.Message = strings.TrimSpace(string(raw))
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, notFoundError{e.Message}
		}
		return nil, errors.New(e.Message)
	}
	return resp, nil
}

// call performs a request and decodes the JSON response into out (if non-nil).
func (d *DockerService) call(ctx context.Context, method, path string, body, out any) error {
	resp, err := d.do(ctx, method, path, body, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func (d *DockerService) Ping(ctx context.Context) error {
	return d.call(ctx, "GET", "/_ping", nil, nil)
}

type SystemInfo struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	DockerVersion string `json:"docker_version"`
	AgentVersion  string `json:"agent_version"`
}

func (d *DockerService) SystemInfo(ctx context.Context) (SystemInfo, error) {
	var info struct {
		Name            string `json:"Name"`
		OperatingSystem string `json:"OperatingSystem"`
	}
	var version struct {
		Version string `json:"Version"`
		Arch    string `json:"Arch"`
	}
	if err := d.call(ctx, "GET", "/info", nil, &info); err != nil {
		return SystemInfo{}, err
	}
	if err := d.call(ctx, "GET", "/version", nil, &version); err != nil {
		return SystemInfo{}, err
	}
	return SystemInfo{Hostname: info.Name, OS: info.OperatingSystem, Arch: version.Arch,
		DockerVersion: version.Version, AgentVersion: agentVersion}, nil
}

// registryAuthHeader builds the X-Registry-Auth header for a pull.
func registryAuthHeader(image string, auths []RegistryAuth) http.Header {
	host := imageRegistry(image)
	for _, a := range auths {
		if a.Server == host {
			b, _ := json.Marshal(map[string]string{"username": a.Username, "password": a.Password, "serveraddress": a.Server})
			return http.Header{"X-Registry-Auth": {base64.URLEncoding.EncodeToString(b)}}
		}
	}
	return nil
}

// imageRegistry returns the registry host of an image ("docker.io" if none).
func imageRegistry(image string) string {
	first, _, ok := strings.Cut(image, "/")
	if ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return first
	}
	return "docker.io"
}

// PullImage downloads an image, calling progress with readable status lines.
// The API streams JSON messages; errors can arrive mid-stream with HTTP 200.
func (d *DockerService) PullImage(ctx context.Context, image string, auths []RegistryAuth, progress func(string)) error {
	name, tag := splitImage(image)
	q := url.Values{"fromImage": {name}}
	if tag != "" {
		q.Set("tag", tag)
	}
	resp, err := d.do(ctx, "POST", "/images/create?"+q.Encode(), nil, registryAuthHeader(image, auths))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var msg struct {
			Status string `json:"status"`
			ID     string `json:"id"`
			Error  string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		if msg.Error != "" {
			return errors.New(msg.Error)
		}
		if progress == nil {
			continue
		}
		// Skip the per-chunk progress noise.
		switch msg.Status {
		case "Downloading", "Extracting", "Waiting", "Verifying Checksum", "Download complete", "Pulling fs layer":
		default:
			if msg.ID != "" {
				progress(msg.ID + ": " + msg.Status)
			} else {
				progress(msg.Status)
			}
		}
	}
	return sc.Err()
}

// splitImage turns "nginx:latest" into ("nginx", "latest"). Images pinned by
// digest are passed whole. Without a tag we default to "latest" (an empty
// tag would make Docker pull every tag).
func splitImage(image string) (name, tag string) {
	if strings.Contains(image, "@") {
		return image, ""
	}
	slash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > slash {
		return image[:colon], image[colon+1:]
	}
	return image, "latest"
}

type ImageInfo struct {
	ID          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
	Config      struct {
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
	} `json:"Config"`
}

func (d *DockerService) InspectImage(ctx context.Context, image string) (ImageInfo, error) {
	var info ImageInfo
	err := d.call(ctx, "GET", "/images/"+url.PathEscape(image)+"/json", nil, &info)
	return info, err
}

// ExposedPorts lists an image's EXPOSEd TCP ports.
func (i ImageInfo) ExposedPorts() []int {
	ports := []int{}
	for k := range i.Config.ExposedPorts {
		p, proto, _ := strings.Cut(k, "/")
		if n, err := strconv.Atoi(p); err == nil && (proto == "" || proto == "tcp") {
			ports = append(ports, n)
		}
	}
	sort.Ints(ports)
	return ports
}

// Digest returns the image's registry digest ("sha256:..."), if known.
func (i ImageInfo) Digest() string {
	for _, rd := range i.RepoDigests {
		if _, digest, ok := strings.Cut(rd, "@"); ok {
			return digest
		}
	}
	return ""
}

func (d *DockerService) TagImage(ctx context.Context, source, target string) error {
	repo, tag := splitImage(target)
	return d.call(ctx, "POST", "/images/"+url.PathEscape(source)+"/tag?"+url.Values{"repo": {repo}, "tag": {tag}}.Encode(), nil, nil)
}

func (d *DockerService) RemoveImage(ctx context.Context, image string) error {
	err := d.call(ctx, "DELETE", "/images/"+url.PathEscape(image), nil, nil)
	if errors.Is(err, errNotFound) {
		return nil
	}
	return err
}

// ListImageTags lists tags in a repository, e.g. "insta-deploy/abcd1234".
func (d *DockerService) ListImageTags(ctx context.Context, repo string) ([]string, error) {
	filters, _ := json.Marshal(map[string][]string{"reference": {repo}})
	var images []ImageInfo
	if err := d.call(ctx, "GET", "/images/json?filters="+url.QueryEscape(string(filters)), nil, &images); err != nil {
		return nil, err
	}
	var tags []string
	for _, img := range images {
		tags = append(tags, img.RepoTags...)
	}
	return tags, nil
}

type ContainerSpec struct {
	Name          string
	Image         string
	Env           []string
	Port          int
	Network       string
	Labels        map[string]string
	Volumes       []VolumeMount
	RestartPolicy string
	CPUs          float64
	MemoryMB      int
}

type VolumeMount struct {
	Source   string // named volume or absolute host path
	Target   string
	ReadOnly bool
}

func (d *DockerService) CreateContainer(ctx context.Context, s ContainerSpec) (string, error) {
	mounts := []map[string]any{}
	for _, v := range s.Volumes {
		typ := "volume"
		if strings.HasPrefix(v.Source, "/") {
			typ = "bind"
		}
		mounts = append(mounts, map[string]any{"Type": typ, "Source": v.Source, "Target": v.Target, "ReadOnly": v.ReadOnly})
	}
	restart := s.RestartPolicy
	if restart == "" {
		restart = "unless-stopped"
	}
	hostConfig := map[string]any{
		"NetworkMode":   s.Network,
		"RestartPolicy": map[string]any{"Name": restart},
		"Mounts":        mounts,
	}
	if s.CPUs > 0 {
		hostConfig["NanoCpus"] = int64(s.CPUs * 1e9)
	}
	if s.MemoryMB > 0 {
		hostConfig["Memory"] = int64(s.MemoryMB) << 20
	}
	body := map[string]any{
		"Image":      s.Image,
		"Env":        s.Env,
		"Labels":     s.Labels,
		"HostConfig": hostConfig,
	}
	if s.Port > 0 {
		body["ExposedPorts"] = map[string]any{fmt.Sprintf("%d/tcp", s.Port): map[string]any{}}
	}
	var out struct {
		ID string `json:"Id"`
	}
	err := d.call(ctx, "POST", "/containers/create?name="+url.QueryEscape(s.Name), body, &out)
	return out.ID, err
}

func (d *DockerService) StartContainer(ctx context.Context, id string) error {
	return d.call(ctx, "POST", "/containers/"+url.PathEscape(id)+"/start", nil, nil)
}

func (d *DockerService) StopContainer(ctx context.Context, id string) error {
	err := d.call(ctx, "POST", "/containers/"+url.PathEscape(id)+"/stop?t=10", nil, nil)
	if errors.Is(err, errNotFound) {
		return nil
	}
	return err
}

func (d *DockerService) RestartContainer(ctx context.Context, id string) error {
	return d.call(ctx, "POST", "/containers/"+url.PathEscape(id)+"/restart?t=10", nil, nil)
}

func (d *DockerService) RemoveContainer(ctx context.Context, id string) error {
	err := d.call(ctx, "DELETE", "/containers/"+url.PathEscape(id)+"?force=true", nil, nil)
	if errors.Is(err, errNotFound) {
		return nil
	}
	return err
}

type ContainerInfo struct {
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	RestartCount int    `json:"RestartCount"`
	State        struct {
		Status   string `json:"Status"`
		ExitCode int    `json:"ExitCode"`
		Error    string `json:"Error"`
		Health   *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	Image           string `json:"Image"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
	Mounts []struct {
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
}

func (d *DockerService) GetContainer(ctx context.Context, id string) (ContainerInfo, error) {
	var info ContainerInfo
	err := d.call(ctx, "GET", "/containers/"+url.PathEscape(id)+"/json", nil, &info)
	return info, err
}

type ContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	State  string            `json:"State"`
	Image  string            `json:"Image"`
	Labels map[string]string `json:"Labels"`
}

// ListContainers lists containers (running or not) matching all the label
// filters, like "insta-deploy.managed=true".
func (d *DockerService) ListContainers(ctx context.Context, labels ...string) ([]ContainerSummary, error) {
	filters, _ := json.Marshal(map[string][]string{"label": labels})
	var out []ContainerSummary
	err := d.call(ctx, "GET", "/containers/json?all=true&filters="+url.QueryEscape(string(filters)), nil, &out)
	return out, err
}

// GetLogs returns the last `tail` lines of a container's output, with
// timestamps, like `docker logs --tail N -t`.
func (d *DockerService) GetLogs(ctx context.Context, id string, tail int) (string, error) {
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}, "tail": {strconv.Itoa(tail)}}
	resp, err := d.do(ctx, "GET", "/containers/"+url.PathEscape(id)+"/logs?"+q.Encode(), nil, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.Header.Get("Content-Type") == "application/vnd.docker.raw-stream" {
		return string(raw), nil // container has a TTY: plain output
	}
	return demuxLogs(raw), nil
}

// demuxLogs strips Docker's stream framing: each frame is an 8-byte header
// (stream type, 3 zero bytes, big-endian length) followed by the payload.
func demuxLogs(raw []byte) string {
	var out strings.Builder
	for len(raw) >= 8 {
		n := int(binary.BigEndian.Uint32(raw[4:8]))
		raw = raw[8:]
		if n > len(raw) {
			n = len(raw)
		}
		out.Write(raw[:n])
		raw = raw[n:]
	}
	return out.String()
}

type ContainerStats struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes uint64  `json:"memory_bytes"`
	MemoryLimit uint64  `json:"memory_limit"`
	NetRxBytes  uint64  `json:"net_rx_bytes"`
	NetTxBytes  uint64  `json:"net_tx_bytes"`
}

// GetStats takes one stats sample. Docker waits about a second to measure
// CPU usage between two readings.
func (d *DockerService) GetStats(ctx context.Context, id string) (ContainerStats, error) {
	var raw struct {
		CPU struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System     uint64 `json:"system_cpu_usage"`
			OnlineCPUs uint64 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPU struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		Memory struct {
			Usage uint64            `json:"usage"`
			Limit uint64            `json:"limit"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
		Networks map[string]struct {
			Rx uint64 `json:"rx_bytes"`
			Tx uint64 `json:"tx_bytes"`
		} `json:"networks"`
	}
	if err := d.call(ctx, "GET", "/containers/"+url.PathEscape(id)+"/stats?stream=false", nil, &raw); err != nil {
		return ContainerStats{}, err
	}
	var s ContainerStats
	cpuDelta := float64(raw.CPU.Usage.Total) - float64(raw.PreCPU.Usage.Total)
	sysDelta := float64(raw.CPU.System) - float64(raw.PreCPU.System)
	if cpuDelta > 0 && sysDelta > 0 {
		cpus := float64(raw.CPU.OnlineCPUs)
		if cpus == 0 {
			cpus = 1
		}
		s.CPUPercent = cpuDelta / sysDelta * cpus * 100
	}
	// Like `docker stats`: don't count reclaimable page cache.
	s.MemoryBytes = raw.Memory.Usage
	if cache := raw.Memory.Stats["inactive_file"]; cache < s.MemoryBytes {
		s.MemoryBytes -= cache
	}
	s.MemoryLimit = raw.Memory.Limit
	for _, n := range raw.Networks {
		s.NetRxBytes += n.Rx
		s.NetTxBytes += n.Tx
	}
	return s, nil
}

// EnsureNetwork creates a bridge network if it doesn't exist. Public
// containers and the tunnel container share it, so the tunnel reaches
// containers by name without publishing any ports on the host.
func (d *DockerService) EnsureNetwork(ctx context.Context, name string) error {
	err := d.call(ctx, "GET", "/networks/"+name, nil, nil)
	if err == nil {
		return nil
	}
	if !errors.Is(err, errNotFound) {
		return err
	}
	return d.call(ctx, "POST", "/networks/create", map[string]any{
		"Name":   name,
		"Driver": "bridge",
		"Labels": map[string]string{labelManaged: "true"},
	}, nil)
}

func (d *DockerService) ConnectNetwork(ctx context.Context, network, container string) error {
	return d.ConnectNetworkAs(ctx, network, container, nil)
}

// ConnectNetworkAs connects a container to a network under extra DNS aliases.
func (d *DockerService) ConnectNetworkAs(ctx context.Context, network, container string, aliases []string) error {
	body := map[string]any{"Container": container}
	if len(aliases) > 0 {
		body["EndpointConfig"] = map[string]any{"Aliases": aliases}
	}
	err := d.call(ctx, "POST", "/networks/"+network+"/connect", body, nil)
	if err != nil && strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return err
}

// RemoveNetwork removes a network by name if nothing uses it any more.
func (d *DockerService) RemoveNetwork(ctx context.Context, name string) {
	d.call(ctx, "DELETE", "/networks/"+name, nil, nil)
}

// RemoveNetworks removes networks with the given label (a Compose project's
// networks). Networks still in use are left alone.
func (d *DockerService) RemoveNetworks(ctx context.Context, label string) {
	filters, _ := json.Marshal(map[string][]string{"label": {label}})
	var nets []struct {
		ID string `json:"Id"`
	}
	if d.call(ctx, "GET", "/networks?filters="+url.QueryEscape(string(filters)), nil, &nets) == nil {
		for _, n := range nets {
			d.call(ctx, "DELETE", "/networks/"+n.ID, nil, nil)
		}
	}
}

func (d *DockerService) RemoveVolume(ctx context.Context, name string) error {
	err := d.call(ctx, "DELETE", "/volumes/"+url.PathEscape(name), nil, nil)
	if errors.Is(err, errNotFound) {
		return nil
	}
	return err
}

// RemoveVolumes removes volumes with the given label (a Compose project's).
func (d *DockerService) RemoveVolumes(ctx context.Context, label string) error {
	filters, _ := json.Marshal(map[string][]string{"label": {label}})
	var out struct {
		Volumes []struct {
			Name string `json:"Name"`
		} `json:"Volumes"`
	}
	if err := d.call(ctx, "GET", "/volumes?filters="+url.QueryEscape(string(filters)), nil, &out); err != nil {
		return err
	}
	for _, v := range out.Volumes {
		if err := d.RemoveVolume(ctx, v.Name); err != nil {
			return err
		}
	}
	return nil
}
