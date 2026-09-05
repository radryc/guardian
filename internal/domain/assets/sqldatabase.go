package assets

import (
	"fmt"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type SQLDatabaseSpec struct {
	Engine   string   `json:"engine" yaml:"engine"`
	Version  string   `json:"version,omitempty" yaml:"version,omitempty"`
	Volume   string   `json:"volume,omitempty" yaml:"volume,omitempty"`
	Config   string   `json:"config,omitempty" yaml:"config,omitempty"`
	Database string   `json:"database,omitempty" yaml:"database,omitempty"`
	User     string   `json:"user,omitempty" yaml:"user,omitempty"`
	Port     *int     `json:"port,omitempty" yaml:"port,omitempty"`
	Networks []string `json:"networks,omitempty" yaml:"networks,omitempty"`
}

type sqlDatabaseDefinition struct {
	typeName string
}

func init() {
	Register(sqlDatabaseDefinition{typeName: assetdomain.TypeDatabase})
	Register(sqlDatabaseDefinition{typeName: assetdomain.TypeSQLDatabase})
}

func (d sqlDatabaseDefinition) Type() string { return d.typeName }

func (d sqlDatabaseDefinition) NewSpec() any { return &SQLDatabaseSpec{} }

func (d sqlDatabaseDefinition) Validate(spec any, ctx ValidationContext) error {
	typed, ok := spec.(*SQLDatabaseSpec)
	if !ok {
		return fmt.Errorf("internal SQL database spec type mismatch")
	}
	if err := requireString(typed.Engine, "engine"); err != nil {
		return err
	}
	if typed.Volume != "" {
		if err := validateAssetRef(ctx, typed.Volume, assetdomain.TypeVolume, "volume"); err != nil {
			return err
		}
	}
	if typed.Config != "" {
		if err := validateAssetRef(ctx, typed.Config, assetdomain.TypeConfig, "config"); err != nil {
			return err
		}
	}
	return optionalPositiveInt(typed.Port, "port")
}
