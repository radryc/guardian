package driverutil

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/store/memory"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func TestResolveConfigFilesRawPayload(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	writeConfigTestFile(t, ctx, store, "/partitions/demo/payloads/prometheus.yml", []byte("global:\n  scrape_interval: 15s\n"))

	in := registry.AssetInput{
		Asset: taskdomain.AbstractAsset{
			Type:    "Config",
			Name:    "prometheus-config",
			Payload: map[string]string{"file": "/partitions/demo/payloads/prometheus.yml"},
		},
		Store: store,
	}
	files, err := ResolveConfigFiles(ctx, in, []string{"k8s", "kubernetes"}, &assetdefs.ConfigSpec{Format: "yaml"})
	if err != nil {
		t.Fatalf("ResolveConfigFiles() error = %v", err)
	}
	if got, want := files.Files["prometheus.yml"], "global:\n  scrape_interval: 15s\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if files.Binary {
		t.Fatal("Binary = true, want false")
	}
}

func TestResolveConfigFilesUniversalFilesPayload(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	writeConfigTestFile(t, ctx, store, "/partitions/demo/payloads/config.yaml", []byte("files:\n  app.yaml: |\n    port: 8080\n  rules.yaml: |\n    rules: []\n"))

	in := registry.AssetInput{
		Asset: taskdomain.AbstractAsset{
			Type:    "Config",
			Name:    "app-config",
			Payload: map[string]string{"files": "/partitions/demo/payloads/config.yaml"},
		},
		Store: store,
	}
	files, err := ResolveConfigFiles(ctx, in, nil, &assetdefs.ConfigSpec{Format: "yaml"})
	if err != nil {
		t.Fatalf("ResolveConfigFiles() error = %v", err)
	}
	if len(files.Files) != 2 || files.Files["app.yaml"] != "port: 8080\n" {
		t.Fatalf("files = %#v", files.Files)
	}
}

func TestResolveConfigFilesLegacyK8sDataPayload(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	writeConfigTestFile(t, ctx, store, "/partitions/demo/payloads/config.k8s.yaml", []byte("data:\n  app.yaml: |\n    port: 8080\n"))

	in := registry.AssetInput{
		Asset: taskdomain.AbstractAsset{
			Type:    "Config",
			Name:    "app-config",
			Payload: map[string]string{"k8s": "/partitions/demo/payloads/config.k8s.yaml"},
		},
		Store: store,
	}
	files, err := ResolveConfigFiles(ctx, in, []string{"k8s", "kubernetes"}, &assetdefs.ConfigSpec{Format: "yaml"})
	if err != nil {
		t.Fatalf("ResolveConfigFiles() error = %v", err)
	}
	if got := files.Files["app.yaml"]; got != "port: 8080\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestResolveConfigFilesBinaryRawPayload(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	raw := []byte{0x00, 0x01, 0xfe, 0xff}
	writeConfigTestFile(t, ctx, store, "/partitions/demo/payloads/blob.bin", raw)

	in := registry.AssetInput{
		Asset: taskdomain.AbstractAsset{
			Type:    "Config",
			Name:    "blob-config",
			Payload: map[string]string{"file": "/partitions/demo/payloads/blob.bin"},
		},
		Store: store,
	}
	files, err := ResolveConfigFiles(ctx, in, nil, &assetdefs.ConfigSpec{})
	if err != nil {
		t.Fatalf("ResolveConfigFiles() error = %v", err)
	}
	if !files.Binary {
		t.Fatal("Binary = false, want inferred true")
	}
	if got := files.Files["blob.bin"]; got != string(raw) {
		t.Fatalf("content = %v, want %v", []byte(got), raw)
	}
}

func TestResolveConfigFilesBinaryInlinePayload(t *testing.T) {
	raw := []byte{0x00, 0x01, 0xfe, 0xff}
	in := registry.AssetInput{
		Asset: taskdomain.AbstractAsset{
			Type: "Config",
			Name: "blob-config",
			Properties: map[string]any{
				"format": "binary",
				"data":   map[string]any{"blob.bin": base64.StdEncoding.EncodeToString(raw)},
			},
		},
	}
	files, err := ResolveConfigFiles(context.Background(), in, nil, &assetdefs.ConfigSpec{
		Format: "binary",
		Data:   map[string]string{"blob.bin": base64.StdEncoding.EncodeToString(raw)},
	})
	if err != nil {
		t.Fatalf("ResolveConfigFiles() error = %v", err)
	}
	if got := files.Files["blob.bin"]; got != string(raw) {
		t.Fatalf("content = %v, want %v", []byte(got), raw)
	}
}

func TestResolveConfigFilesRejectsRawLegacyWrapper(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	writeConfigTestFile(t, ctx, store, "/partitions/demo/payloads/config.k8s.yaml", []byte("global:\n  scrape_interval: 15s\n"))

	in := registry.AssetInput{
		Asset: taskdomain.AbstractAsset{
			Type:    "Config",
			Name:    "prometheus-config",
			Payload: map[string]string{"k8s": "/partitions/demo/payloads/config.k8s.yaml"},
		},
		Store: store,
	}
	if _, err := ResolveConfigFiles(ctx, in, []string{"k8s", "kubernetes"}, &assetdefs.ConfigSpec{Format: "yaml"}); err == nil {
		t.Fatal("ResolveConfigFiles() error = nil, want structured payload error")
	} else if !strings.Contains(err.Error(), "files: or data:") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveConfigFilesRejectsUniversalKeyMix(t *testing.T) {
	in := registry.AssetInput{
		Asset: taskdomain.AbstractAsset{
			Type: "Config",
			Name: "app-config",
			Payload: map[string]string{
				"file":  "/partitions/demo/payloads/app.yaml",
				"files": "/partitions/demo/payloads/files.yaml",
			},
		},
	}
	if _, err := ResolveConfigFiles(context.Background(), in, nil, &assetdefs.ConfigSpec{}); err == nil {
		t.Fatal("ResolveConfigFiles() error = nil, want mutual exclusion error")
	}
}

func writeConfigTestFile(t *testing.T, ctx context.Context, store *memory.Store, logicalPath string, content []byte) {
	t.Helper()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: "test"},
	}); err != nil {
		t.Fatalf("write %s: %v", logicalPath, err)
	}
}
