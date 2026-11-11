package bootstrap

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireEnvsubst(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("envsubst"); err != nil {
		t.Skip("envsubst is required for bootstrap template tests")
	}
}

func writeTemplate(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deploy.yaml")
	content := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: node-${SUFFIX}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func TestApplyTemplatesForEachWaitWaitsBetweenSuffixes(t *testing.T) {
	requireEnvsubst(t)
	templatePath := writeTemplate(t)
	var order []string

	err := ApplyTemplatesForEachWait(
		context.Background(),
		templatePath,
		Env{},
		func(suffix string) Env { return Env{"SUFFIX": suffix} },
		[]string{"a", "b", "c"},
		true,
		func(_ context.Context, suffix string) error {
			order = append(order, suffix)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ApplyTemplatesForEachWait() error = %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(order) != len(want) {
		t.Fatalf("wait order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("wait order = %v, want %v", order, want)
		}
	}
}

func TestApplyTemplatesForEachWaitStopsWhenWaitFails(t *testing.T) {
	requireEnvsubst(t)
	templatePath := writeTemplate(t)
	applied := 0

	err := ApplyTemplatesForEachWait(
		context.Background(),
		templatePath,
		Env{},
		func(suffix string) Env { return Env{"SUFFIX": suffix} },
		[]string{"a", "b", "c"},
		true,
		func(_ context.Context, suffix string) error {
			applied++
			if suffix == "a" {
				return errors.New("not ready")
			}
			return nil
		},
	)
	if err == nil {
		t.Fatal("expected ApplyTemplatesForEachWait() to fail")
	}
	if applied != 1 {
		t.Fatalf("wait callback ran %d times, want 1 (must stop after failure)", applied)
	}
}
