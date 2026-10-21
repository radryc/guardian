package cdktemplate

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// parseYAMLTemplate decodes a CloudFormation YAML template into the same
// JSON-compatible shape as a JSON template. Short-form intrinsic tags (!Ref,
// !GetAtt, !Sub, !Join, ...) are expanded to their long form so comparison and
// inventory see the same references regardless of the source encoding.
func parseYAMLTemplate(raw string) (map[string]any, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
		return nil, fmt.Errorf("template is neither JSON nor YAML: %w", err)
	}
	value, err := yamlNodeValue(&root)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return map[string]any{}, nil
	}
	template, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("template root must be a mapping")
	}
	return template, nil
}

func yamlNodeValue(node *yaml.Node) (any, error) {
	if node == nil {
		return nil, nil
	}
	if strings.HasPrefix(node.Tag, "!") && !strings.HasPrefix(node.Tag, "!!") {
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, fmt.Errorf("decode YAML intrinsic %s: %w", node.Tag, err)
		}
		name := strings.TrimPrefix(node.Tag, "!")
		if name == "GetAtt" {
			if text, ok := value.(string); ok {
				if parts := strings.SplitN(text, ".", 2); len(parts) == 2 {
					return map[string]any{"Fn::GetAtt": []any{parts[0], parts[1]}}, nil
				}
			}
		}
		key := name
		if name != "Ref" && name != "Condition" {
			key = "Fn::" + name
		}
		return map[string]any{key: value}, nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return map[string]any{}, nil
		}
		return yamlNodeValue(node.Content[0])
	case yaml.MappingNode:
		out := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, err := yamlNodeValue(node.Content[i])
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				name = fmt.Sprint(key)
			}
			value, err := yamlNodeValue(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[name] = value
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := yamlNodeValue(child)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	case yaml.AliasNode:
		return yamlNodeValue(node.Alias)
	case yaml.ScalarNode:
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported YAML node kind %d", node.Kind)
	}
}
