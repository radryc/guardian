package awsdriver

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTagArgsSkipsEmptyValues(t *testing.T) {
	args := tagArgs(map[string]string{
		"guardian.managed":   "true",
		"guardian.cluster":   "",
		"guardian.partition": "demo-deploy",
		"guardian.hash":      "   ",
	})
	want := []string{
		"--tags", "guardian.managed=true",
		"--tags", "guardian.partition=demo-deploy",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("tagArgs = %v, want %v", args, want)
	}
}

func TestTagArgsStableOrder(t *testing.T) {
	args := tagArgs(map[string]string{"b": "2", "a": "1"})
	want := []string{"--tags", "a=1", "--tags", "b=2"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("tagArgs = %v, want %v", args, want)
	}
}

func TestPackageManagerCommandUsesIgnoreScripts(t *testing.T) {
	cases := []struct {
		name        string
		files       []string
		preferred   string
		wantManager string
		wantArgs    []string
	}{
		{
			name:        "npm install without lockfile",
			files:       []string{"package.json"},
			preferred:   "npm",
			wantManager: "npm",
			wantArgs:    []string{"install", "--ignore-scripts"},
		},
		{
			name:        "npm ci with package-lock.json",
			files:       []string{"package.json", "package-lock.json"},
			preferred:   "",
			wantManager: "npm",
			wantArgs:    []string{"ci", "--ignore-scripts"},
		},
		{
			name:        "pnpm install with lockfile",
			files:       []string{"package.json", "pnpm-lock.yaml"},
			preferred:   "",
			wantManager: "pnpm",
			wantArgs:    []string{"install", "--frozen-lockfile", "--ignore-scripts"},
		},
		{
			name:        "pnpm install without lockfile",
			files:       []string{"package.json"},
			preferred:   "pnpm",
			wantManager: "pnpm",
			wantArgs:    []string{"install", "--ignore-scripts"},
		},
		{
			name:        "yarn install with lockfile",
			files:       []string{"package.json", "yarn.lock"},
			preferred:   "",
			wantManager: "yarn",
			wantArgs:    []string{"install", "--frozen-lockfile", "--ignore-scripts"},
		},
		{
			name:        "yarn install without lockfile",
			files:       []string{"package.json"},
			preferred:   "yarn",
			wantManager: "yarn",
			wantArgs:    []string{"install", "--ignore-scripts"},
		},
		{
			name:        "none skips install",
			files:       []string{"package.json"},
			preferred:   "none",
			wantManager: "",
			wantArgs:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("{}"), 0o644); err != nil {
					t.Fatalf("write %s: %v", f, err)
				}
			}
			manager, args, err := packageManagerCommand(dir, tc.preferred)
			if err != nil {
				t.Fatalf("packageManagerCommand returned error: %v", err)
			}
			if manager != tc.wantManager || !reflect.DeepEqual(args, tc.wantArgs) {
				t.Fatalf("packageManagerCommand = (%q, %v), want (%q, %v)", manager, args, tc.wantManager, tc.wantArgs)
			}
		})
	}
}
