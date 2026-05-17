package main

import "testing"

func TestParseAddHosts(t *testing.T) {
	got, err := parseAddHosts("host.docker.internal:host-gateway,example.internal=10.0.0.10")
	if err != nil {
		t.Fatalf("parseAddHosts() error = %v", err)
	}
	if got["host.docker.internal"] != "host-gateway" {
		t.Fatalf("host.docker.internal = %q", got["host.docker.internal"])
	}
	if got["example.internal"] != "10.0.0.10" {
		t.Fatalf("example.internal = %q", got["example.internal"])
	}
}

func TestParseAddHostsRejectsInvalidMapping(t *testing.T) {
	if _, err := parseAddHosts("missing-separator"); err == nil {
		t.Fatal("parseAddHosts() error = nil, want error")
	}
}
