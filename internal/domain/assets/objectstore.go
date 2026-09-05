package assets

import (
	"fmt"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type ObjectStoreSpec struct {
	Engine          string   `json:"engine" yaml:"engine"`
	Endpoint        string   `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Volume          string   `json:"volume,omitempty" yaml:"volume,omitempty"`
	Config          string   `json:"config,omitempty" yaml:"config,omitempty"`
	Buckets         []string `json:"buckets,omitempty" yaml:"buckets,omitempty"`
	Networks        []string `json:"networks,omitempty" yaml:"networks,omitempty"`
	Region          string   `json:"region,omitempty" yaml:"region,omitempty"`
	AccessKeyID     string   `json:"accessKeyID,omitempty" yaml:"accessKeyID,omitempty"`
	SecretAccessKey string   `json:"secretAccessKey,omitempty" yaml:"secretAccessKey,omitempty"`
	UsePathStyle    *bool    `json:"usePathStyle,omitempty" yaml:"usePathStyle,omitempty"`
	Versioning      *bool    `json:"versioning,omitempty" yaml:"versioning,omitempty"`
}

type objectStoreDefinition struct{}

func init() {
	Register(objectStoreDefinition{})
}

func (objectStoreDefinition) Type() string { return assetdomain.TypeObjectStore }

func (objectStoreDefinition) NewSpec() any { return &ObjectStoreSpec{} }

func (objectStoreDefinition) Validate(spec any, ctx ValidationContext) error {
	typed, ok := spec.(*ObjectStoreSpec)
	if !ok {
		return fmt.Errorf("internal object store spec type mismatch")
	}
	if err := requireString(typed.Engine, "engine"); err != nil {
		return err
	}
	if typed.Endpoint == "" && typed.Volume != "" {
		if err := validateAssetRef(ctx, typed.Volume, assetdomain.TypeVolume, "volume"); err != nil {
			return err
		}
	}
	if typed.Endpoint == "" && typed.Config != "" {
		if err := validateAssetRef(ctx, typed.Config, assetdomain.TypeConfig, "config"); err != nil {
			return err
		}
	}
	if (typed.AccessKeyID == "") != (typed.SecretAccessKey == "") {
		return fmt.Errorf("accessKeyID and secretAccessKey must be provided together")
	}
	return validateStringList(typed.Buckets, "buckets")
}
