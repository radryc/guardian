package kubernetesdriver

import (
	"reflect"
	"testing"
)

func deploymentWithEnv(containers ...map[string]any) map[string]any {
	containerList := make([]any, len(containers))
	for i, container := range containers {
		containerList[i] = container
	}
	return map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": containerList,
				},
			},
		},
	}
}

func TestDeploymentEnvConflictPatchReplacesStaleValueFrom(t *testing.T) {
	desired := deploymentWithEnv(map[string]any{
		"name": "app",
		"env": []any{
			map[string]any{"name": "TOKEN", "value": "literal"},
			map[string]any{"name": "OTHER", "value": "unchanged"},
		},
	})
	live := deploymentWithEnv(map[string]any{
		"name": "app",
		"env": []any{
			map[string]any{"name": "TOKEN", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "s", "key": "k"}}},
			map[string]any{"name": "OTHER", "value": "unchanged"},
		},
	})

	patch, err := deploymentEnvConflictPatch(desired, live)
	if err != nil {
		t.Fatalf("deploymentEnvConflictPatch() error = %v", err)
	}
	want := []map[string]any{{
		"op":    "replace",
		"path":  "/spec/template/spec/containers/0/env/0",
		"value": map[string]any{"name": "TOKEN", "value": "literal"},
	}}
	if !reflect.DeepEqual(patch, want) {
		t.Fatalf("patch = %#v, want %#v", patch, want)
	}
}

func TestDeploymentEnvConflictPatchSkipsNonConflicts(t *testing.T) {
	desired := deploymentWithEnv(map[string]any{
		"name": "app",
		"env": []any{
			map[string]any{"name": "TOKEN", "value": "literal"},
			map[string]any{"name": "FROM_SECRET", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "s", "key": "k"}}},
		},
	})
	live := deploymentWithEnv(map[string]any{
		"name": "app",
		"env": []any{
			map[string]any{"name": "TOKEN", "value": "different"},
			map[string]any{"name": "FROM_SECRET", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "s", "key": "k"}}},
		},
	})

	patch, err := deploymentEnvConflictPatch(desired, live)
	if err != nil {
		t.Fatalf("deploymentEnvConflictPatch() error = %v", err)
	}
	if len(patch) != 0 {
		t.Fatalf("patch = %#v, want no operations", patch)
	}
}

func TestDeploymentEnvConflictPatchMatchesContainersByName(t *testing.T) {
	desired := deploymentWithEnv(
		map[string]any{"name": "sidecar", "env": []any{map[string]any{"name": "TOKEN", "value": "sidecar-value"}}},
		map[string]any{"name": "app", "env": []any{map[string]any{"name": "TOKEN", "value": "app-value"}}},
	)
	live := deploymentWithEnv(
		map[string]any{"name": "app", "env": []any{
			map[string]any{"name": "TOKEN", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "s", "key": "k"}}},
		}},
		map[string]any{"name": "sidecar", "env": []any{
			map[string]any{"name": "TOKEN", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "s", "key": "k"}}},
		}},
	)

	patch, err := deploymentEnvConflictPatch(desired, live)
	if err != nil {
		t.Fatalf("deploymentEnvConflictPatch() error = %v", err)
	}
	want := []map[string]any{
		{
			"op":    "replace",
			"path":  "/spec/template/spec/containers/1/env/0",
			"value": map[string]any{"name": "TOKEN", "value": "sidecar-value"},
		},
		{
			"op":    "replace",
			"path":  "/spec/template/spec/containers/0/env/0",
			"value": map[string]any{"name": "TOKEN", "value": "app-value"},
		},
	}
	if !reflect.DeepEqual(patch, want) {
		t.Fatalf("patch = %#v, want %#v", patch, want)
	}
}

func TestDeploymentEnvConflictPatchHandlesMissingShapes(t *testing.T) {
	cases := []struct {
		name    string
		desired map[string]any
		live    map[string]any
	}{
		{name: "empty", desired: map[string]any{}, live: map[string]any{}},
		{name: "no desired containers", desired: map[string]any{}, live: deploymentWithEnv(map[string]any{"name": "app"})},
		{name: "no live containers", desired: deploymentWithEnv(map[string]any{"name": "app"}), live: map[string]any{}},
		{name: "no live env", desired: deploymentWithEnv(map[string]any{"name": "app", "env": []any{map[string]any{"name": "TOKEN", "value": "v"}}}), live: deploymentWithEnv(map[string]any{"name": "app"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			patch, err := deploymentEnvConflictPatch(tc.desired, tc.live)
			if err != nil {
				t.Fatalf("deploymentEnvConflictPatch() error = %v", err)
			}
			if len(patch) != 0 {
				t.Fatalf("patch = %#v, want no operations", patch)
			}
		})
	}
}

func TestNormalizeManifestConvertsTypedEnvList(t *testing.T) {
	manifest := deploymentWithEnv(map[string]any{
		"name": "app",
		"env":  []map[string]string{{"name": "TOKEN", "value": "literal"}},
	})
	normalized, err := normalizeManifest(manifest)
	if err != nil {
		t.Fatalf("normalizeManifest() error = %v", err)
	}
	if _, ok := normalized["spec"].(map[string]any); !ok {
		t.Fatalf("normalized manifest has unexpected shape: %#v", normalized)
	}
	containers, ok := nestedSlice(normalized, "spec", "template", "spec", "containers")
	if !ok || len(containers) != 1 {
		t.Fatalf("containers = %#v, ok = %v", containers, ok)
	}
	env, ok := containers[0].(map[string]any)["env"].([]any)
	if !ok || len(env) != 1 {
		t.Fatalf("env = %#v, ok = %v", env, ok)
	}
}
