package resolver

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var outputRefPattern = regexp.MustCompile(`\$\{intent\.([a-zA-Z0-9-]+)\.outputs\.([a-zA-Z0-9_.-]+)\}`)

type OutputRef struct {
	IntentName  string
	OutputKey   string
	Placeholder string
}

func FindRefs(properties map[string]any) []OutputRef {
	found := map[string]OutputRef{}
	walkRefs(properties, found)
	out := make([]OutputRef, 0, len(found))
	for _, ref := range found {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IntentName == out[j].IntentName {
			if out[i].OutputKey == out[j].OutputKey {
				return out[i].Placeholder < out[j].Placeholder
			}
			return out[i].OutputKey < out[j].OutputKey
		}
		return out[i].IntentName < out[j].IntentName
	})
	return out
}

func ResolveProperties(properties map[string]any, outputs map[string]map[string]string) (map[string]any, error) {
	if properties == nil {
		return nil, nil
	}
	resolved, err := resolveValue(properties, outputs)
	if err != nil {
		return nil, err
	}
	cast, ok := resolved.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("resolved properties had unexpected type %T", resolved)
	}
	return cast, nil
}

func walkRefs(value any, found map[string]OutputRef) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			walkRefs(typed[key], found)
		}
	case []any:
		for _, item := range typed {
			walkRefs(item, found)
		}
	case string:
		matches := outputRefPattern.FindAllStringSubmatch(typed, -1)
		for _, match := range matches {
			ref := OutputRef{IntentName: match[1], OutputKey: match[2], Placeholder: match[0]}
			found[ref.Placeholder] = ref
		}
	}
}

func resolveValue(value any, outputs map[string]map[string]string) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			resolved, err := resolveValue(typed[key], outputs)
			if err != nil {
				return nil, err
			}
			out[key] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			resolved, err := resolveValue(item, outputs)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	case string:
		return resolveString(typed, outputs)
	default:
		return typed, nil
	}
}

func resolveString(in string, outputs map[string]map[string]string) (string, error) {
	matches := outputRefPattern.FindAllStringSubmatch(in, -1)
	resolved := in
	for _, match := range matches {
		placeholder := match[0]
		intentName := match[1]
		outputKey := match[2]
		intentOutputs, ok := outputs[intentName]
		if !ok {
			return "", fmt.Errorf("missing outputs for intent %q", intentName)
		}
		outputValue, ok := intentOutputs[outputKey]
		if !ok {
			return "", fmt.Errorf("missing output %q for intent %q", outputKey, intentName)
		}
		resolved = strings.ReplaceAll(resolved, placeholder, outputValue)
	}
	return resolved, nil
}
