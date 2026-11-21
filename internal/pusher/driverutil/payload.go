package driverutil

import (
	"context"
	"fmt"
	"sort"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/pusher/secrets"
	"gopkg.in/yaml.v3"
)

func DecodeAsset(in registry.AssetInput) (any, error) {
	typed, _, err := assetdefs.Decode(assetdomain.Spec{
		Type:       in.Asset.Type,
		Name:       in.Asset.Name,
		DependsOn:  append([]string(nil), in.Asset.DependsOn...),
		Properties: cloneMap(in.Asset.Properties),
	})
	if err != nil {
		return nil, err
	}
	return typed, nil
}

func DecodeNamedAsset(in registry.AssetInput, name string) (taskdomain.AbstractAsset, any, error) {
	asset, ok := in.Assets[name]
	if !ok {
		return taskdomain.AbstractAsset{}, nil, fmt.Errorf("referenced asset %q not found", name)
	}
	typed, _, err := assetdefs.Decode(assetdomain.Spec{
		Type:       asset.Type,
		Name:       asset.Name,
		DependsOn:  append([]string(nil), asset.DependsOn...),
		Properties: cloneMap(asset.Properties),
	})
	if err != nil {
		return taskdomain.AbstractAsset{}, nil, err
	}
	return asset, typed, nil
}

func ResolveEnv(ctx context.Context, resolver secrets.Resolver, values map[string]any) (map[string]string, error) {
	return secrets.ResolveStringMap(ctx, resolver, values)
}

func ConfigFiles(spec *assetdefs.ConfigSpec) map[string]string {
	if spec == nil {
		return map[string]string{}
	}
	if len(spec.Data) > 0 {
		keys := make([]string, 0, len(spec.Data))
		for key := range spec.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]string, len(keys))
		for _, key := range keys {
			out[key] = spec.Data[key]
		}
		return out
	}
	name := "config" + extensionForFormat(spec.Format)
	return map[string]string{name: spec.Content}
}

func SingleConfigFile(spec *assetdefs.ConfigSpec) (string, string, bool) {
	files := ConfigFiles(spec)
	if len(files) != 1 {
		return "", "", false
	}
	for name, content := range files {
		return name, content, true
	}
	return "", "", false
}

func BoolValue(v *bool) bool {
	return v != nil && *v
}

func IntValue(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

func LoadPayload[T any](ctx context.Context, in registry.AssetInput, providerKeys []string, out *T) (bool, error) {
	if out == nil {
		return false, fmt.Errorf("payload decode target is nil")
	}
	if in.Store == nil || len(in.Asset.Payload) == 0 {
		return false, nil
	}
	logicalPath := ""
	for _, key := range providerKeys {
		if key == "" {
			continue
		}
		if value := strings.TrimSpace(in.Asset.Payload[key]); value != "" {
			logicalPath = value
			break
		}
	}
	if logicalPath == "" {
		return false, nil
	}
	content, err := in.Store.ReadFile(ctx, logicalPath)
	if err != nil {
		return false, fmt.Errorf("read payload %s: %w", logicalPath, err)
	}
	if err := yaml.Unmarshal(content, out); err != nil {
		return false, fmt.Errorf("decode payload %s: %w", logicalPath, err)
	}
	return true, nil
}

func extensionForFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return ".json"
	case "yaml", "yml":
		return ".yaml"
	case "toml":
		return ".toml"
	case "ini":
		return ".ini"
	case "cfg", "conf":
		return ".cfg"
	case "text", "txt":
		return ".txt"
	case "binary", "bin":
		return ".bin"
	default:
		return ".txt"
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(in any) any {
	switch typed := in.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = cloneValue(value)
		}
		return out
	default:
		return typed
	}
}

// MaxServiceNameLength is kept for callers that need the identity cap.

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
