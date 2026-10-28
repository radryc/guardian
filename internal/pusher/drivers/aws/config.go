package awsdriver

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/driverutil"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/versioning/digest"
)

type ConfigDriver struct{ baseDriver }

func (d *ConfigDriver) Type() string                    { return "Config" }
func (d *ConfigDriver) Validate(p map[string]any) error { return nil }

func configParameterName(spec *assetdefs.ConfigSpec, in registry.AssetInput) string {
	if name := strings.TrimSpace(spec.ExistingParameter); name != "" {
		return name
	}
	return awsSSMParameterName(in, in.Asset.Name)
}

func (d *ConfigDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeConfig(in)
	if err != nil {
		return err
	}
	_, _, err = d.backend.GetParameter(ctx, configParameterName(spec, in))
	return err
}

func (d *ConfigDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeConfig(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	files, err := driverutil.ResolveConfigFiles(ctx, in, []string{"aws"}, spec)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	hash := configContentHash(in, files)
	data := configFileValue(files)

	name := configParameterName(spec, in)
	param, ok, err := d.backend.GetParameter(ctx, name)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok {
		if spec.ExistingParameter != "" {
			return changedDrift(in.Asset.Name, "adopted SSM parameter is missing"), nil
		}
		return changedDrift(in.Asset.Name, "SSM parameter differs"), nil
	}
	if param.Hash != "" && param.Hash != hash {
		return changedDrift(in.Asset.Name, "SSM parameter differs"), nil
	}
	if param.Value != data {
		return changedDrift(in.Asset.Name, "SSM parameter differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "SSM parameter is in sync"), nil
}

func (d *ConfigDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeConfig(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	files, err := driverutil.ResolveConfigFiles(ctx, in, []string{"aws"}, spec)
	if err != nil {
		return registry.AssetResult{}, err
	}

	hash := configContentHash(in, files)
	value := configFileValue(files)
	paramType := "String"
	if !files.Binary && containsSensitiveKeys(files.Files) {
		paramType = "SecureString"
	}

	name := configParameterName(spec, in)

	tags := awsTags(in, hash)
	if err := d.backend.UpsertParameter(ctx, Parameter{
		Name:  name,
		Type:  paramType,
		Value: value,
		Hash:  hash,
		Tags:  tags,
	}); err != nil {
		return registry.AssetResult{}, fmt.Errorf("upsert SSM parameter %s: %w", name, err)
	}

	return registry.AssetResult{Outputs: map[string]string{"name": name, "arn": "ssm:" + name}}, nil
}

func (d *ConfigDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeConfig(in)
	if err != nil {
		return err
	}
	return d.backend.DeleteParameter(ctx, configParameterName(spec, in))
}

func decodeConfig(in registry.AssetInput) (*assetdefs.ConfigSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.ConfigSpec)
	if !ok {
		return nil, fmt.Errorf("expected ConfigSpec, got %T", typed)
	}
	return spec, nil
}

func configContentHash(in registry.AssetInput, files driverutil.ConfigFileSet) string {
	return digest.MustNormalizedHash(struct {
		Base   string
		Files  map[string]string
		Binary bool
	}{
		Base:   driverutil.AssetHash(in),
		Files:  files.HashContent(),
		Binary: files.Binary,
	})
}

func configFileValue(files driverutil.ConfigFileSet) string {
	value := contentAsString(files.Files)
	if files.Binary {
		return base64.StdEncoding.EncodeToString([]byte(value))
	}
	return value
}

func contentAsString(data map[string]string) string {
	if len(data) == 1 {
		for _, v := range data {
			return v
		}
	}
	names := make([]string, 0, len(data))
	for name := range data {
		names = append(names, name)
	}
	sort.Strings(names)
	result := ""
	for _, name := range names {
		result += name + ":\n" + data[name] + "\n---\n"
	}
	return result
}

func containsSensitiveKeys(data map[string]string) bool {
	for k := range data {
		lower := toLower(k)
		if contains(lower, "secret") || contains(lower, "password") || contains(lower, "token") || contains(lower, "key") || contains(lower, "credential") {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	lower := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		lower[i] = c
	}
	return string(lower)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
