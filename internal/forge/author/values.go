package author

import (
	"fmt"
	"os"
	"path/filepath"

	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	"gopkg.in/yaml.v3"
)

// SetValues writes a values map at the partition, intent, or asset level.
// Leave intentName empty for the partition, and assetName empty for the intent.
// A nil map removes the values block at that level.
func SetValues(partitionDir, intentName, assetName string, vals map[string]any) (string, error) {
	if intentName == "" && assetName != "" {
		return "", fmt.Errorf("asset values require an intent")
	}
	if intentName == "" {
		path, err := ConfigPath(partitionDir)
		if err != nil {
			return "", err
		}
		doc, err := loadDocument(path)
		if err != nil {
			return "", err
		}
		root, err := rootMapping(doc)
		if err != nil {
			return "", err
		}
		spec, err := mappingChild(root, "spec", true)
		if err != nil {
			return "", err
		}
		if err := setValuesNode(spec, vals); err != nil {
			return "", err
		}
		return path, savePartition(path, doc)
	}

	path := filepath.Join(partitionDir, "intents", intentName+".yaml")
	doc, err := loadDocument(path)
	if err != nil {
		return "", err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return "", err
	}
	spec, err := mappingChild(root, "spec", true)
	if err != nil {
		return "", err
	}
	if assetName == "" {
		if err := setValuesNode(spec, vals); err != nil {
			return "", err
		}
		return path, saveIntent(path, doc)
	}
	assets, err := assetsSequence(doc)
	if err != nil {
		return "", err
	}
	asset := findAssetNode(assets, assetName)
	if asset == nil {
		return "", fmt.Errorf("asset %q not found in %s", assetName, path)
	}
	if err := setValuesNode(asset, vals); err != nil {
		return "", err
	}
	return path, saveIntent(path, doc)
}

func setValuesNode(mapping *yaml.Node, vals map[string]any) error {
	if len(vals) == 0 {
		removeMappingKey(mapping, "values")
		return nil
	}
	node, err := nodeFromAny(vals)
	if err != nil {
		return err
	}
	setMapping(mapping, "values", node)
	return nil
}

// ReadValues returns the values map at the partition, intent, or asset level.
func ReadValues(partitionDir, intentName, assetName string) (map[string]any, error) {
	if intentName == "" {
		part, _, err := ReadPartition(partitionDir)
		if err != nil {
			return nil, err
		}
		return part.Spec.Values, nil
	}
	path := filepath.Join(partitionDir, "intents", intentName+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var intent intentdomain.Intent
	if err := yaml.Unmarshal(data, &intent); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if assetName == "" {
		return intent.Spec.Values, nil
	}
	for i := range intent.Spec.Assets {
		if intent.Spec.Assets[i].Name == assetName {
			return intent.Spec.Assets[i].Values, nil
		}
	}
	return nil, fmt.Errorf("asset %q not found in %s", assetName, path)
}
