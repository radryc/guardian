package manifest

import (
	"fmt"

	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	"gopkg.in/yaml.v3"
)

func ParsePartition(data []byte) (*partitiondomain.Partition, error) {
	var part partitiondomain.Partition
	if err := yaml.Unmarshal(data, &part); err != nil {
		return nil, fmt.Errorf("parse partition: %w", err)
	}
	if part.APIVersion != "guardian/v1alpha1" {
		return nil, fmt.Errorf("unsupported partition apiVersion %q", part.APIVersion)
	}
	if part.Kind != "Partition" {
		return nil, fmt.Errorf("unexpected kind %q", part.Kind)
	}
	return &part, nil
}

func ParseIntent(data []byte) (*intentdomain.Intent, error) {
	var in intentdomain.Intent
	if err := yaml.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("parse intent: %w", err)
	}
	if in.APIVersion != "guardian/v1alpha1" {
		return nil, fmt.Errorf("unsupported intent apiVersion %q", in.APIVersion)
	}
	if in.Kind != "Intent" {
		return nil, fmt.Errorf("unexpected kind %q", in.Kind)
	}
	if in.Spec.IntentType == "" {
		in.Spec.IntentType = "standard"
	}
	return &in, nil
}
