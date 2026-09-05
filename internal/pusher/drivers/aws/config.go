package awsdriver

import (
	"context"
	"fmt"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

type ConfigDriver struct{ baseDriver }

func (d *ConfigDriver) Type() string                { return "Config" }
func (d *ConfigDriver) Validate(p map[string]any) error { return nil }

func (d *ConfigDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name := awsSSMParameterName(in, in.Asset.Name)
	_, _, err := d.backend.GetParameter(ctx, name)
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

	hash := driverutil.CompositeHash(in)
	data := driverutil.ConfigFiles(spec)

	name := awsSSMParameterName(in, in.Asset.Name)
	param, ok, err := d.backend.GetParameter(ctx, name)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || param.Hash != hash || param.Value != contentAsString(data) {
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

	hash := driverutil.CompositeHash(in)
	data := driverutil.ConfigFiles(spec)
	value := contentAsString(data)
	paramType := "String"
	isSensitive := containsSensitiveKeys(data)

	name := awsSSMParameterName(in, in.Asset.Name)
	if isSensitive {
		paramType = "SecureString"
	}

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
	return d.backend.DeleteParameter(ctx, awsSSMParameterName(in, in.Asset.Name))
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

func contentAsString(data map[string]string) string {
	if len(data) == 1 {
		for _, v := range data {
			return v
		}
	}
	result := ""
	for k, v := range data {
		result += k + ":\n" + v + "\n---\n"
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
