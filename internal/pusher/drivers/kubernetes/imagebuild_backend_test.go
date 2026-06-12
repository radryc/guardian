package kubernetesdriver

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateBuildContextArchiveIncludesRootFiles(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "nginx.conf"), []byte("server {}\n"), 0o644); err != nil {
		t.Fatalf("write nginx.conf: %v", err)
	}
	if err := os.Mkdir(filepath.Join(workspaceDir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "src", "main.tsx"), []byte("console.log('ok')\n"), 0o644); err != nil {
		t.Fatalf("write src/main.tsx: %v", err)
	}

	archivePath, cleanup, err := createBuildContextArchive(workspaceDir)
	if err != nil {
		t.Fatalf("createBuildContextArchive() error = %v", err)
	}
	defer cleanup()

	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)

	seen := map[string]bool{}
	for {
		header, err := tarReader.Next()
		if err != nil {
			break
		}
		seen[header.Name] = true
	}
	for _, name := range []string{"Dockerfile", "nginx.conf", "src/", "src/main.tsx"} {
		if !seen[name] {
			t.Fatalf("archive missing %s; seen=%v", name, seen)
		}
	}
}
