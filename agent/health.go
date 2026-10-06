package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// HTTP health checks for services that asked for one (and have no Docker
// HEALTHCHECK). The settings live in container labels, so they survive
// agent restarts without any local state.

type healthChecker struct {
	docker *DockerService
	client *http.Client

	mu      sync.Mutex
	results map[string]string    // container ID -> HEALTHY / UNHEALTHY
	lastRun map[string]time.Time // container ID -> last check
}

func newHealthChecker(d *DockerService) *healthChecker {
	return &healthChecker{
		docker:  d,
		client:  &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		results: map[string]string{},
		lastRun: map[string]time.Time{},
	}
}

func (h *healthChecker) result(containerID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.results[containerID]
}

func (h *healthChecker) run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			h.checkAll(ctx)
		}
	}
}

func (h *healthChecker) checkAll(ctx context.Context) {
	containers, err := h.docker.ListContainers(ctx, labelManaged+"=true", labelHealthPath)
	if err != nil {
		return
	}
	for _, c := range containers {
		every, _ := strconv.Atoi(c.Labels[labelHealthEvery])
		if every < 5 {
			every = 30
		}
		h.mu.Lock()
		due := time.Since(h.lastRun[c.ID]) >= time.Duration(every)*time.Second
		if due {
			h.lastRun[c.ID] = time.Now()
		}
		h.mu.Unlock()
		if !due || c.State != "running" {
			continue
		}
		go h.check(ctx, c)
	}
}

// check requests http://<container IP>:<port><path>. Any status below 400
// counts as healthy.
func (h *healthChecker) check(ctx context.Context, c ContainerSummary) {
	status := "UNHEALTHY"
	defer func() {
		h.mu.Lock()
		h.results[c.ID] = status
		h.mu.Unlock()
	}()
	info, err := h.docker.GetContainer(ctx, c.ID)
	if err != nil {
		return
	}
	// Prefer the shared network, which the agent is also connected to.
	ip := info.NetworkSettings.Networks[networkName].IPAddress
	for _, n := range info.NetworkSettings.Networks {
		if ip == "" {
			ip = n.IPAddress
		}
	}
	if ip == "" {
		return
	}
	url := fmt.Sprintf("http://%s:%s%s", ip, c.Labels[labelHealthPort], c.Labels[labelHealthPath])
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "insta-deploy-healthcheck")
	resp, err := h.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
	if resp.StatusCode < 400 {
		status = "HEALTHY"
	}
}
