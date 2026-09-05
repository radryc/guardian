package awsdriver

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
)

type stackPayload struct {
	SourceType          string            `yaml:"sourceType,omitempty"`
	SourceDir           string            `yaml:"sourceDir,omitempty"`
	PrebuiltAssemblyDir string            `yaml:"prebuiltAssemblyDir,omitempty"`
	AppCommand          string            `yaml:"appCommand,omitempty"`
	Entrypoint          string            `yaml:"entrypoint,omitempty"`
	StackName           string            `yaml:"stackName,omitempty"`
	StackID             string            `yaml:"stackID,omitempty"`
	PackageManager      string            `yaml:"packageManager,omitempty"`
	Context             map[string]string `yaml:"context,omitempty"`
	OutputMap           map[string]string `yaml:"outputMap,omitempty"`
}

func loadStackPayload(ctx context.Context, in registry.AssetInput) (stackPayload, error) {
	var payload stackPayload
	loaded, err := driverutil.LoadPayload(ctx, in, []string{"aws"}, &payload)
	if err != nil {
		return stackPayload{}, err
	}
	if !loaded {
		return stackPayload{}, fmt.Errorf("asset %q payload.aws is required", in.Asset.Name)
	}
	if err := payload.validate(); err != nil {
		return stackPayload{}, err
	}
	return payload.normalized(), nil
}

func (p stackPayload) normalized() stackPayload {
	p.SourceType = strings.TrimSpace(p.SourceType)
	p.SourceDir = cleanLogicalPath(p.SourceDir)
	p.PrebuiltAssemblyDir = cleanLogicalPath(p.PrebuiltAssemblyDir)
	p.AppCommand = strings.TrimSpace(p.AppCommand)
	p.Entrypoint = strings.TrimSpace(p.Entrypoint)
	p.StackName = strings.TrimSpace(p.StackName)
	p.StackID = strings.TrimSpace(p.StackID)
	p.PackageManager = strings.ToLower(strings.TrimSpace(p.PackageManager))
	p.Context = normalizeStringMap(p.Context)
	p.OutputMap = normalizeStringMap(p.OutputMap)
	return p
}

func cleanLogicalPath(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	return path.Clean(trimmed)
}

func (p stackPayload) validate() error {
	p = p.normalized()
	if p.SourceType != "cdk-ts" {
		return fmt.Errorf("property sourceType must be %q", "cdk-ts")
	}
	hasSourceDir := p.SourceDir != ""
	hasPrebuiltAssembly := p.PrebuiltAssemblyDir != ""
	if hasSourceDir == hasPrebuiltAssembly {
		return fmt.Errorf("exactly one of sourceDir or prebuiltAssemblyDir is required")
	}
	if hasSourceDir && !strings.HasPrefix(p.SourceDir, "/") {
		return fmt.Errorf("property sourceDir must be an absolute logical path")
	}
	if hasPrebuiltAssembly && !strings.HasPrefix(p.PrebuiltAssemblyDir, "/") {
		return fmt.Errorf("property prebuiltAssemblyDir must be an absolute logical path")
	}
	if p.StackName == "" {
		return fmt.Errorf("property stackName is required")
	}
	if p.StackID == "" {
		return fmt.Errorf("property stackID is required")
	}
	switch p.PackageManager {
	case "", "npm", "pnpm", "yarn", "none":
	default:
		return fmt.Errorf("property packageManager must be one of npm, pnpm, yarn, none")
	}
	for key, value := range p.Context {
		if key == "" {
			return fmt.Errorf("property context must not contain empty keys")
		}
		if value == "" {
			return fmt.Errorf("property context[%q] must not be empty", key)
		}
	}
	for alias, outputKey := range p.OutputMap {
		if alias == "" {
			return fmt.Errorf("property outputMap must not contain empty keys")
		}
		if outputKey == "" {
			return fmt.Errorf("property outputMap[%q] must not be empty", alias)
		}
	}
	return nil
}

type sourceFileSnapshot struct {
	Path    string
	Content string
}

func desiredHash(in registry.AssetInput, payload stackPayload, context map[string]string, env map[string]string, files []sourceFileSnapshot) string {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return digest.MustNormalizedHash(struct {
		Base    string
		Payload stackPayload
		Context map[string]string
		Env     map[string]string
		Source  []sourceFileSnapshot
	}{
		Base:    driverutil.AssetHash(in),
		Payload: payload,
		Context: normalizeStringMap(context),
		Env:     normalizeStringMap(env),
		Source:  files,
	})
}

func normalizeStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		value := strings.TrimSpace(in[key])
		if value == "" {
			continue
		}
		out[trimmedKey] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
