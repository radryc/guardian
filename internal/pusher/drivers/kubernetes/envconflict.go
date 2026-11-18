package kubernetesdriver

import (
	"encoding/json"
	"fmt"
)

// normalizeManifest converts a manifest built by the drivers (which may contain
// typed slices such as []map[string]string) into the generic map/slice shape
// returned by the Kubernetes API. It lets the same conflict detection run in
// both the client-go and kubectl backends.
func normalizeManifest(obj map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return out, nil
}

// deploymentEnvConflictPatch returns JSON Patch operations that replace live
// env entries which still carry a valueFrom (usually a manually-added
// secretKeyRef) with the literal value from the desired manifest.
//
// Kubernetes rejects an EnvVar that has both value and valueFrom. Server-side
// apply does not remove fields owned by another field manager, so a stale
// valueFrom can survive even though Guardian's desired manifest only contains
// value. Patching the conflicting entries first keeps apply idempotent without
// replacing the whole Deployment or dropping external annotations.
func deploymentEnvConflictPatch(desired, live map[string]any) ([]map[string]any, error) {
	desiredContainers, ok := nestedSlice(desired, "spec", "template", "spec", "containers")
	if !ok {
		return nil, nil
	}
	liveContainers, ok := nestedSlice(live, "spec", "template", "spec", "containers")
	if !ok {
		return nil, nil
	}

	liveContainerIndex := map[string]int{}
	for i, raw := range liveContainers {
		container, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := container["name"].(string); name != "" {
			liveContainerIndex[name] = i
		}
	}

	var ops []map[string]any
	for _, raw := range desiredContainers {
		container, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := container["name"].(string)
		containerIndex, ok := liveContainerIndex[name]
		if !ok {
			continue
		}

		desiredEnv, _ := container["env"].([]any)
		if len(desiredEnv) == 0 {
			continue
		}
		liveContainer, ok := liveContainers[containerIndex].(map[string]any)
		if !ok {
			continue
		}
		liveEnv, _ := liveContainer["env"].([]any)
		liveEnvIndex := map[string]int{}
		for i, rawEnv := range liveEnv {
			env, ok := rawEnv.(map[string]any)
			if !ok {
				continue
			}
			if envName, _ := env["name"].(string); envName != "" {
				liveEnvIndex[envName] = i
			}
		}

		for _, rawEnv := range desiredEnv {
			env, ok := rawEnv.(map[string]any)
			if !ok {
				continue
			}
			if _, hasValue := env["value"]; !hasValue {
				continue
			}
			// A malformed desired entry with both fields cannot be repaired
			// here; leave it for validation to report.
			if _, hasValueFrom := env["valueFrom"]; hasValueFrom {
				continue
			}
			envName, _ := env["name"].(string)
			envIndex, ok := liveEnvIndex[envName]
			if !ok {
				continue
			}
			liveEnvEntry, ok := liveEnv[envIndex].(map[string]any)
			if !ok {
				continue
			}
			if _, hasValueFrom := liveEnvEntry["valueFrom"]; !hasValueFrom {
				continue
			}
			ops = append(ops, map[string]any{
				"op":    "replace",
				"path":  fmt.Sprintf("/spec/template/spec/containers/%d/env/%d", containerIndex, envIndex),
				"value": env,
			})
		}
	}
	return ops, nil
}

func nestedSlice(obj map[string]any, fields ...string) ([]any, bool) {
	var current any = obj
	for _, field := range fields {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = asMap[field]
		if !ok {
			return nil, false
		}
	}
	slice, ok := current.([]any)
	return slice, ok
}
