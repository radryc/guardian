package discover

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunDetectsWorkloads(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module example.com/demo\n\ngo 1.26\n")
	write(t, filepath.Join(root, "Dockerfile"), `FROM golang:1.26
EXPOSE 8080
ENV DATABASE_URL=
HEALTHCHECK CMD curl -f http://localhost:8080/healthz
`)
	write(t, filepath.Join(root, "docker-compose.yml"), `services:
  api:
    image: example/api:1
    ports:
      - "8080:8080"
    environment:
      - LOG_LEVEL=info
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/healthz"]
  db:
    image: postgres:16
    ports:
      - "5432:5432"
`)
	write(t, filepath.Join(root, "k8s", "deploy.yaml"), `apiVersion: apps/v1
kind: Deployment
metadata:
  name: worker
spec:
  template:
    spec:
      containers:
        - name: worker
          image: example/worker:1
          ports:
            - containerPort: 9000
          env:
            - name: QUEUE_URL
          readinessProbe:
            httpGet:
              path: /ready
`)
	write(t, filepath.Join(root, ".guardian", "config.yaml"), `apiVersion: guardian/v1alpha1
kind: RepoConfig
repo:
  name: demo
  source: github.com/acme/demo
  branch: main
`)
	write(t, filepath.Join(root, "node_modules", "ignored", "Dockerfile"), "FROM node:20\nEXPOSE 3000\n")

	profile, err := Run(context.Background(), Options{Root: root})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if profile.Repository == nil || profile.Repository.Name != "demo" || profile.Repository.Source != "github.com/acme/demo" {
		t.Fatalf("repository = %+v", profile.Repository)
	}
	if len(profile.Languages) != 1 || profile.Languages[0] != "go" {
		t.Fatalf("languages = %v", profile.Languages)
	}

	byKey := map[string]Workload{}
	for _, workload := range profile.Workloads {
		byKey[workload.Kind+"|"+workload.Name] = workload
	}
	if _, ok := byKey["dockerfile|"+filepath.Base(root)]; !ok {
		t.Errorf("root Dockerfile workload missing: %v", keys(byKey))
	}
	api, ok := byKey["compose|api"]
	if !ok {
		t.Fatalf("compose api missing: %v", keys(byKey))
	}
	if len(api.Ports) != 1 || api.Ports[0] != 8080 {
		t.Errorf("compose ports = %v", api.Ports)
	}
	if len(api.Env) != 1 || api.Env[0] != "LOG_LEVEL" {
		t.Errorf("compose env = %v", api.Env)
	}
	if len(api.HealthPaths) != 1 || api.HealthPaths[0] != "/healthz" {
		t.Errorf("compose health = %v", api.HealthPaths)
	}
	if _, ok := byKey["compose|db"]; !ok {
		t.Errorf("compose db missing: %v", keys(byKey))
	}
	worker, ok := byKey["kubernetes|worker"]
	if !ok {
		t.Fatalf("kubernetes worker missing: %v", keys(byKey))
	}
	if len(worker.Ports) != 1 || worker.Ports[0] != 9000 {
		t.Errorf("k8s ports = %v", worker.Ports)
	}
	if len(worker.HealthPaths) != 1 || worker.HealthPaths[0] != "/ready" {
		t.Errorf("k8s health = %v", worker.HealthPaths)
	}
	if worker.Evidence[0] == "" {
		t.Error("evidence is required")
	}

	for _, workload := range profile.Workloads {
		if workload.Ports != nil && len(workload.Ports) == 1 && workload.Ports[0] == 3000 {
			t.Fatal("node_modules must be skipped")
		}
	}
	if len(profile.Suggestions) != len(profile.Workloads) {
		t.Errorf("suggestions = %v", profile.Suggestions)
	}
}

func keys(values map[string]Workload) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	return out
}
