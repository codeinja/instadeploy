package main

import "testing"

func TestTransformComposeJoinsProjectNetwork(t *testing.T) {
	p := composeProject{
		"services": map[string]any{
			"legion":   map[string]any{"image": "legion"},
			"postgres": map[string]any{"image": "postgres", "networks": map[string]any{"db": nil}},
			"sidecar":  map[string]any{"image": "x", "network_mode": "service:legion"},
		},
	}
	d := &Deployment{ID: "d1", Name: "legion", Network: "insta-deploy", ProjectNetwork: "insta-p-1234abcd-production",
		Services: []Service{{Name: "legion", Public: true, Alias: "insta-legion-1-legion", ProjectAliases: []string{"legion.legion"}}}}
	if err := transformCompose(p, d); err != nil {
		t.Fatal(err)
	}
	svcs := p.services()
	aliases := func(svc, network string) any {
		n, _ := svcs[svc]["networks"].(map[string]any)
		cfg, _ := n[network].(map[string]any)
		if cfg == nil {
			return nil
		}
		return cfg["aliases"]
	}
	if got := aliases("legion", d.ProjectNetwork); got.([]string)[0] != "legion.legion" {
		t.Fatalf("legion project alias: %v", got)
	}
	if got := aliases("legion", d.Network); got == nil {
		t.Fatal("public service left the tunnel network")
	}
	// Services not described by the backend still get "<service>.<deployment>", and keep
	// their own networks.
	if got := aliases("postgres", d.ProjectNetwork); got.([]string)[0] != "postgres.legion" {
		t.Fatalf("postgres project alias: %v", got)
	}
	if _, ok := svcs["postgres"]["networks"].(map[string]any)["db"]; !ok {
		t.Fatal("postgres lost its own network")
	}
	if _, ok := svcs["postgres"]["networks"].(map[string]any)[d.Network]; ok {
		t.Fatal("a private service joined the tunnel network")
	}
	if _, ok := svcs["sidecar"]["networks"]; ok {
		t.Fatal("a network_mode service cannot join networks")
	}
	nets := p["networks"].(map[string]any)
	if n := nets[d.ProjectNetwork].(map[string]any); n["external"] != true || n["name"] != d.ProjectNetwork {
		t.Fatalf("project network declaration: %v", n)
	}
}

func TestTransformComposeWithoutProjectNetwork(t *testing.T) {
	p := composeProject{"services": map[string]any{"web": map[string]any{"image": "nginx"}}}
	if err := transformCompose(p, &Deployment{Name: "web", Network: "insta-deploy"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := p["networks"]; ok {
		t.Fatal("older backends (no project network) must not add networks")
	}
}
