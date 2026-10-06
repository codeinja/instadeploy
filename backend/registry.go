package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Inspecting images through the registry's HTTP API (the same API
// `docker pull` uses) tells us an image's exposed ports and digest without
// downloading it, and without the backend touching Docker.

var registryHostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]+)?$`)

func normalizeRegistry(server string) string {
	server = strings.ToLower(strings.TrimSpace(server))
	server = strings.TrimPrefix(strings.TrimPrefix(server, "https://"), "http://")
	server = strings.TrimSuffix(server, "/")
	switch server {
	case "index.docker.io", "registry-1.docker.io", "hub.docker.com":
		return "docker.io"
	}
	return server
}

type imageRef struct {
	Registry string // "docker.io", "ghcr.io", ...
	Repo     string // "library/nginx"
	Tag      string
	Digest   string
}

func parseImageRef(image string) imageRef {
	ref := imageRef{Registry: "docker.io"}
	rest := image
	if i := strings.Index(rest, "@"); i >= 0 {
		ref.Digest = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		first := rest[:i]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			ref.Registry = first
			rest = rest[i+1:]
		}
	}
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		ref.Tag = rest[i+1:]
		rest = rest[:i]
	}
	if ref.Registry == "docker.io" && !strings.Contains(rest, "/") {
		rest = "library/" + rest
	}
	ref.Repo = rest
	if ref.Tag == "" && ref.Digest == "" {
		ref.Tag = "latest"
	}
	return ref
}

// imageRepository strips the tag and digest: "nginx:1.27" -> "nginx".
func imageRepository(image string) string {
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	slash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > slash {
		image = image[:colon]
	}
	return image
}

type ImageInfo struct {
	Image        string `json:"image"`
	Digest       string `json:"digest"`
	ExposedPorts []int  `json:"exposed_ports"`
}

const manifestAccept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

// inspectImage fetches an image's config from its registry.
func inspectImage(ctx context.Context, image string, auths []RegistryAuth, arch string) (ImageInfo, error) {
	ref := parseImageRef(image)
	host := ref.Registry
	if host == "docker.io" {
		host = "registry-1.docker.io"
	}
	c := &registryClient{base: "https://" + host + "/v2/" + ref.Repo}
	for _, a := range auths {
		if normalizeRegistry(a.Server) == ref.Registry {
			c.user, c.pass = a.Username, a.Password
		}
	}
	if arch == "" {
		arch = "amd64"
	}

	reference := ref.Tag
	if ref.Digest != "" {
		reference = ref.Digest
	}
	var manifest struct {
		MediaType string `json:"mediaType"`
		Config    struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	digest, err := c.getJSON(ctx, "/manifests/"+reference, manifestAccept, &manifest)
	if err != nil {
		return ImageInfo{}, err
	}
	info := ImageInfo{Image: image, Digest: digest}

	// A multi-platform image: pick the agent's platform.
	if len(manifest.Manifests) > 0 {
		chosen := ""
		for _, m := range manifest.Manifests {
			if m.Platform.OS == "linux" && m.Platform.Architecture == arch {
				chosen = m.Digest
				break
			}
		}
		if chosen == "" {
			return info, fmt.Errorf("image %s has no linux/%s version", image, arch)
		}
		if _, err := c.getJSON(ctx, "/manifests/"+chosen, manifestAccept, &manifest); err != nil {
			return info, err
		}
	}
	if manifest.Config.Digest == "" {
		return info, errors.New("unexpected manifest format")
	}

	var config struct {
		Config struct {
			ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		} `json:"config"`
	}
	if _, err := c.getJSON(ctx, "/blobs/"+manifest.Config.Digest, "", &config); err != nil {
		return info, err
	}
	info.ExposedPorts = parseExposedPorts(config.Config.ExposedPorts)
	return info, nil
}

// parseExposedPorts turns {"80/tcp": {}, "53/udp": {}} into [80] (TCP only:
// public routes are HTTP).
func parseExposedPorts(m map[string]struct{}) []int {
	ports := []int{}
	for k := range m {
		port, proto, _ := strings.Cut(k, "/")
		if proto != "" && proto != "tcp" {
			continue
		}
		if n, err := strconv.Atoi(port); err == nil && validPort(n) {
			ports = append(ports, n)
		}
	}
	sort.Ints(ports)
	return ports
}

type registryClient struct {
	base       string
	user, pass string
	token      string
}

// getJSON GETs base+path, handling the registry's token authentication
// (a 401 tells us where to get a token). Returns the content digest.
func (c *registryClient) getJSON(ctx context.Context, path, accept string, out any) (string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		h := http.Header{}
		if accept != "" {
			h.Set("Accept", accept)
		}
		if c.token != "" {
			h.Set("Authorization", "Bearer "+c.token)
		} else if c.user != "" {
			h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.user+":"+c.pass)))
		}
		resp, err := safeGet(ctx, c.base+path, h)
		if err != nil {
			return "", fmt.Errorf("the registry is not reachable: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusUnauthorized && attempt == 0:
			if err := c.authenticate(ctx, resp.Header.Get("WWW-Authenticate")); err != nil {
				return "", err
			}
			continue
		case resp.StatusCode == http.StatusNotFound:
			return "", errors.New("image not found. Check the name and tag")
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return "", errors.New("access denied. The image may be private: add registry credentials in Settings")
		case resp.StatusCode >= 300:
			return "", fmt.Errorf("the registry returned %d", resp.StatusCode)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return "", fmt.Errorf("unexpected registry response: %w", err)
		}
		return resp.Header.Get("Docker-Content-Digest"), nil
	}
	return "", errors.New("registry authentication failed")
}

var authParamRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

func (c *registryClient) authenticate(ctx context.Context, challenge string) error {
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		if c.user != "" {
			return errors.New("access denied: check the registry credentials in Settings")
		}
		return errors.New("access denied. The image may be private: add registry credentials in Settings")
	}
	params := map[string]string{}
	for _, m := range authParamRe.FindAllStringSubmatch(challenge, -1) {
		params[m[1]] = m[2]
	}
	if !strings.HasPrefix(params["realm"], "https://") {
		return errors.New("registry asked for authentication at an unsupported address")
	}
	url := params["realm"] + "?service=" + params["service"] + "&scope=" + params["scope"]
	h := http.Header{}
	if c.user != "" {
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.user+":"+c.pass)))
	}
	resp, err := safeGet(ctx, url, h)
	if err != nil {
		return fmt.Errorf("registry authentication failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errors.New("registry authentication failed: check the credentials in Settings")
	}
	var tok struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok)
	c.token = tok.Token
	if c.token == "" {
		c.token = tok.AccessToken
	}
	if c.token == "" {
		return errors.New("registry authentication failed")
	}
	return nil
}
