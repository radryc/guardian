package opentofu

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type call struct {
	Name string
	Args []string
}

type fakeRunner struct {
	calls []call
	fn    func(name string, args []string) ([]byte, []byte, error)
}

func (f *fakeRunner) Run(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, call{Name: name, Args: append([]string(nil), args...)})
	if f.fn == nil {
		return nil, nil, nil
	}
	return f.fn(name, args)
}

func (f *fakeRunner) last() call {
	if len(f.calls) == 0 {
		return call{}
	}
	return f.calls[len(f.calls)-1]
}

func newTestBackend(t *testing.T, fn func(string, []string) ([]byte, []byte, error)) (*Backend, *fakeRunner) {
	t.Helper()
	runner := &fakeRunner{fn: fn}
	backend, err := NewWithRunner(Config{
		WorkRoot:        t.TempDir(),
		StateBucket:     "state-bucket",
		StatePrefixRoot: "guardian",
		StateKMSKey:     "projects/p/locations/r/keyRings/kr/cryptoKeys/k",
		KeepWorkspace:   true,
	}, runner)
	if err != nil {
		t.Fatalf("NewWithRunner: %v", err)
	}
	return backend, runner
}

func TestOpenRequiresStateBucket(t *testing.T) {
	backend, err := NewWithRunner(Config{WorkRoot: t.TempDir()}, &fakeRunner{})
	if err != nil {
		t.Fatalf("NewWithRunner: %v", err)
	}
	if _, err := backend.Open("p", "i"); err == nil {
		t.Fatal("expected an error for a missing state bucket")
	}
}

func TestInitWritesBackendAndEncryption(t *testing.T) {
	backend, runner := newTestBackend(t, nil)
	workspace, err := backend.Open("demo", "api")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer workspace.Close()

	if err := workspace.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(workspace.Dir(), backendFile))
	if err != nil {
		t.Fatalf("read backend file: %v", err)
	}
	for _, want := range []string{
		`backend "gcs" {}`,
		`key_provider "gcp_kms" "state"`,
		`kms_encryption_key = "projects/p/locations/r/keyRings/kr/cryptoKeys/k"`,
		`key_length         = 32`,
		`method "aes_gcm" "state"`,
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("backend config missing %q:\n%s", want, content)
		}
	}

	last := runner.last()
	if last.Name != "tofu" {
		t.Fatalf("runner name = %q, want tofu", last.Name)
	}
	joined := strings.Join(last.Args, " ")
	for _, want := range []string{
		"init",
		"-backend-config=bucket=state-bucket",
		"-backend-config=prefix=guardian/demo/api",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("init args missing %q: %v", want, last.Args)
		}
	}
}

func TestInitSkipsEncryptionWithoutKey(t *testing.T) {
	runner := &fakeRunner{}
	backend, err := NewWithRunner(Config{
		WorkRoot:      t.TempDir(),
		StateBucket:   "state-bucket",
		KeepWorkspace: true,
	}, runner)
	if err != nil {
		t.Fatalf("NewWithRunner: %v", err)
	}
	workspace, err := backend.Open("p", "i")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer workspace.Close()
	if err := workspace.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(workspace.Dir(), backendFile))
	if strings.Contains(string(content), "encryption") {
		t.Errorf("expected no encryption block without a KMS key:\n%s", content)
	}
}

func TestWriteBlueprintRejectsTraversal(t *testing.T) {
	backend, _ := newTestBackend(t, nil)
	workspace, err := backend.Open("p", "i")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer workspace.Close()

	for _, name := range []string{"../escape.tf", "/etc/passwd", ".."} {
		if err := workspace.WriteBlueprint(map[string]string{name: "x"}); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
	if err := workspace.WriteBlueprint(map[string]string{"modules/network/main.tf": "resource {}"}); err != nil {
		t.Fatalf("valid blueprint rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace.Dir(), "modules/network/main.tf")); err != nil {
		t.Fatalf("blueprint file missing: %v", err)
	}
}

func TestWriteVariables(t *testing.T) {
	backend, _ := newTestBackend(t, nil)
	workspace, err := backend.Open("p", "i")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer workspace.Close()

	if err := workspace.WriteVariables(map[string]any{"project": "demo", "replicas": 2}); err != nil {
		t.Fatalf("WriteVariables: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(workspace.Dir(), variablesFile))
	if err != nil {
		t.Fatalf("read variables: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("decode variables: %v", err)
	}
	want := map[string]any{"project": "demo", "replicas": float64(2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("variables = %#v, want %#v", got, want)
	}
}

func TestPlanDriftExitCode(t *testing.T) {
	backend, _ := newTestBackend(t, func(_ string, args []string) ([]byte, []byte, error) {
		if !contains(args, "plan") {
			return nil, nil, nil
		}
		line := `{"@level":"info","type":"change_summary","changes":{"add":1,"change":0,"remove":2,"import":0}}`
		return []byte(line + "\n"), nil, &ExitError{Code: 2, Err: errors.New("exit status 2")}
	})
	workspace, err := backend.Open("p", "i")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer workspace.Close()

	result, err := workspace.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !result.Drift {
		t.Fatal("expected drift")
	}
	if result.Add != 1 || result.Remove != 2 || result.Change != 0 {
		t.Fatalf("unexpected changes: %+v", result)
	}
}

func TestPlanNoChanges(t *testing.T) {
	backend, _ := newTestBackend(t, func(_ string, args []string) ([]byte, []byte, error) {
		if !contains(args, "plan") {
			return nil, nil, nil
		}
		return []byte(`{"type":"change_summary","changes":{"add":0,"change":0,"remove":0}}` + "\n"), nil, nil
	})
	workspace, err := backend.Open("p", "i")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer workspace.Close()

	result, err := workspace.Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if result.Drift {
		t.Fatal("did not expect drift")
	}
}

func TestPlanFailureIncludesDiagnostic(t *testing.T) {
	backend, _ := newTestBackend(t, func(_ string, args []string) ([]byte, []byte, error) {
		if !contains(args, "plan") {
			return nil, nil, nil
		}
		line := `{"type":"diagnostic","diagnostic":{"severity":"error","summary":"invalid provider","detail":"bad credentials"}}`
		return []byte(line + "\n"), []byte("boom"), &ExitError{Code: 1, Err: errors.New("exit status 1")}
	})
	workspace, err := backend.Open("p", "i")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer workspace.Close()

	result, err := workspace.Plan(context.Background())
	if err == nil {
		t.Fatal("expected a plan error")
	}
	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0], "invalid provider") {
		t.Fatalf("diagnostics = %v", result.Diagnostics)
	}
	if !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("error missing plan output: %v", err)
	}
}

func TestOutputDecodesScalars(t *testing.T) {
	backend, _ := newTestBackend(t, func(_ string, args []string) ([]byte, []byte, error) {
		if !contains(args, "output") {
			return nil, nil, nil
		}
		return []byte(`{"bucket":{"value":"my-bucket","sensitive":false},"port":{"value":8080},"replicas":{"value":["a","b"]}}`), nil, nil
	})
	workspace, err := backend.Open("p", "i")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer workspace.Close()

	out, err := workspace.Output(context.Background())
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if out["bucket"] != "my-bucket" {
		t.Errorf("bucket = %q", out["bucket"])
	}
	if out["port"] != "8080" {
		t.Errorf("port = %q", out["port"])
	}
	if out["replicas"] != `["a","b"]` {
		t.Errorf("replicas = %q", out["replicas"])
	}
}

func TestSecondOpenIsLocked(t *testing.T) {
	backend, _ := newTestBackend(t, nil)
	first, err := backend.Open("p", "i")
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	defer first.Close()

	if _, err := backend.Open("p", "i"); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Open error = %v, want ErrLocked", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	third, err := backend.Open("p", "i")
	if err != nil {
		t.Fatalf("Open after Close: %v", err)
	}
	_ = third.Close()
}

func TestCloseRemovesWorkspace(t *testing.T) {
	runner := &fakeRunner{}
	backend, err := NewWithRunner(Config{
		WorkRoot:    t.TempDir(),
		StateBucket: "state-bucket",
	}, runner)
	if err != nil {
		t.Fatalf("NewWithRunner: %v", err)
	}
	workspace, err := backend.Open("p", "i")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := workspace.Dir()
	if err := workspace.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace still exists: %v", err)
	}
}

func TestStatePrefixSanitizesSegments(t *testing.T) {
	backend, _ := newTestBackend(t, nil)
	if got := backend.StatePrefix("../demo", "a/b"); strings.Contains(got, "..") || strings.Contains(got, "//") {
		t.Fatalf("unsafe prefix: %q", got)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestStatePrefixAndWorkspaceScopeEachAsset(t *testing.T) {
	backend, _ := newTestBackend(t, nil)
	legacy := backend.StatePrefix("p", "i")
	web := backend.StatePrefix("p", "i", "web")
	db := backend.StatePrefix("p", "i", "db")
	if web == db {
		t.Fatalf("assets share a state prefix: %q", web)
	}
	if web == legacy || db == legacy {
		t.Fatalf("asset prefix must not reuse the legacy intent prefix: %q", legacy)
	}
	if !strings.HasPrefix(web, legacy+"/") {
		t.Fatalf("asset prefix %q not rooted at %q", web, legacy)
	}
	if backend.WorkspaceDir("p", "i", "web") == backend.WorkspaceDir("p", "i", "db") {
		t.Fatal("assets share a workspace directory")
	}
}
