package assets

import (
	"encoding/json"
	"fmt"
	"sort"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type ValidationContext struct {
	AssetTypes map[string]string
}

type Definition interface {
	Type() string
	NewSpec() any
	Validate(spec any, ctx ValidationContext) error
}

var definitions = map[string]Definition{}

func Register(def Definition) {
	if def == nil {
		return
	}
	definitions[def.Type()] = def
}

func DefinitionFor(assetType string) (Definition, bool) {
	def, ok := definitions[assetType]
	return def, ok
}

func KnownTypes() []string {
	keys := make([]string, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func Decode(spec assetdomain.Spec) (any, Definition, error) {
	def, ok := DefinitionFor(spec.Type)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported asset type %q", spec.Type)
	}
	target := def.NewSpec()
	props := spec.Properties
	if props == nil {
		props = map[string]any{}
	}
	content, err := json.Marshal(props)
	if err != nil {
		return nil, nil, fmt.Errorf("encode asset properties: %w", err)
	}
	if err := json.Unmarshal(content, target); err != nil {
		return nil, nil, fmt.Errorf("decode %s properties: %w", spec.Type, err)
	}
	return target, def, nil
}

func Validate(spec assetdomain.Spec, ctx ValidationContext) error {
	typed, def, err := Decode(spec)
	if err != nil {
		return err
	}
	return def.Validate(typed, ctx)
}
