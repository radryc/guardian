package observedgen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
)

func writeAssembly(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{
  "version": "36.0.0",
  "artifacts": {
    "AppStack": {
      "type": "aws:cloudformation:stack",
      "properties": {"templateFile": "AppStack.template.json", "stackName": "AppStack"}
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AppStack.template.json"), []byte(`{"Resources":{"Bucket":{"Type":"AWS::S3::Bucket"}}}`), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	assetDir := filepath.Join(dir, "asset.abc")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatalf("mkdir asset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "bundle.zip"), []byte("asset"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	return dir
}

func TestGenerateSelectsIntentTypeByMode(t *testing.T) {
	assembly := writeAssembly(t)
	cases := []struct {
		mode Mode
		want string
	}{
		{Mode(""), "observed"},
		{ModeObserved, "observed"},
		{ModeManaged, "standard"},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			out := t.TempDir()
			created, err := Generate(Options{
				Mode:        tc.mode,
				AssemblyDir: assembly,
				OutDir:      out,
				Partition:   "demo",
				Account:     "123456789012",
				Region:      "eu-west-1",
				Pusher:      "aws-123456789012",
			})
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if len(created) != 1 || created[0] != "app-stack" {
				t.Fatalf("created = %v", created)
			}
			content, err := os.ReadFile(filepath.Join(out, "intents", "app-stack.yaml"))
			if err != nil {
				t.Fatalf("read intent: %v", err)
			}
			intent, err := manifest.ParseIntent(content)
			if err != nil {
				t.Fatalf("parse intent: %v", err)
			}
			if intent.Spec.IntentType != tc.want {
				t.Fatalf("intentType = %q, want %q", intent.Spec.IntentType, tc.want)
			}
			_, statErr := os.Stat(filepath.Join(out, "payloads", "aws", "cdk.out", "asset.abc", "bundle.zip"))
			if tc.want == "standard" && statErr != nil {
				t.Fatalf("managed bundle must include staged assets: %v", statErr)
			}
			if tc.want == "observed" && statErr == nil {
				t.Fatal("observed bundle must not copy staged assets")
			}
		})
	}
}

func TestAssignIntentNamesDeduplicates(t *testing.T) {
	names := assignIntentNames([]assemblyStack{
		{ID: "A", Name: "Foo-Bar"},
		{ID: "B", Name: "FooBar"},
		{ID: "C", Name: "Other"},
	})
	want := []string{"foo-bar", "foo-bar-2", "other"}
	if len(names) != len(want) {
		t.Fatalf("names = %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}

func TestGenerateRejectsUnknownMode(t *testing.T) {
	if _, err := Generate(Options{
		Mode:        Mode("bogus"),
		AssemblyDir: writeAssembly(t),
		OutDir:      t.TempDir(),
		Partition:   "demo",
		Account:     "123456789012",
		Region:      "eu-west-1",
	}); err == nil {
		t.Fatal("expected unsupported mode error")
	}
}
