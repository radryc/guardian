// Package values resolves ${value.NAME} placeholders in Guardian manifests.
//
// Values are defined at partition, intent, and asset level. Intent values
// override partition values, and asset values override both. Resolution runs
// during compile, so the compiled bundle and every pusher see concrete values.
package values

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	embeddedRef = regexp.MustCompile(`\$\{value\.([A-Za-z_][A-Za-z0-9_.-]*)\}`)
	exactRef    = regexp.MustCompile(`^\$\{value\.([A-Za-z_][A-Za-z0-9_.-]*)\}$`)
)

// Merge returns a new map with override applied on top of base. Nested maps are
// merged recursively; every other value is replaced.
func Merge(base, override map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range override {
		if nested, ok := value.(map[string]any); ok {
			if existing, ok := out[key].(map[string]any); ok {
				out[key] = Merge(existing, nested)
				continue
			}
		}
		out[key] = value
	}
	return out
}

// Resolve replaces ${value.NAME} references in strings, maps, and slices.
func Resolve(value any, vals map[string]any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		return ResolveMap(typed, vals)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			resolved, err := Resolve(item, vals)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	case string:
		return ResolveValueString(typed, vals)
	default:
		return value, nil
	}
}

// ResolveMap resolves every value in a map. Keys are never rewritten.
func ResolveMap(in map[string]any, vals map[string]any) (map[string]any, error) {
	if in == nil {
		return nil, nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		resolved, err := Resolve(value, vals)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = resolved
	}
	return out, nil
}

// ResolveStringMap resolves every value in a string map.
func ResolveStringMap(in map[string]string, vals map[string]any) (map[string]string, error) {
	if in == nil {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		resolved, err := ResolveString(value, vals)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = resolved
	}
	return out, nil
}

// ResolveString resolves one string. A string that is exactly one reference
// keeps the referenced value's type; embedded references are stringified.
func ResolveString(in string, vals map[string]any) (string, error) {
	resolved, err := ResolveValueString(in, vals)
	if err != nil {
		return "", err
	}
	switch typed := resolved.(type) {
	case string:
		return typed, nil
	case fmt.Stringer:
		return typed.String(), nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(typed), nil
	default:
		return "", fmt.Errorf("value %q is not a string", in)
	}
}

// ResolveValueString is ResolveString but preserves the type of an exact
// reference, which lets ${value.replicas} produce a number.
func ResolveValueString(in string, vals map[string]any) (any, error) {
	if match := exactRef.FindStringSubmatch(in); match != nil {
		value, ok := lookup(vals, match[1])
		if !ok {
			return nil, unknown(match[1], vals)
		}
		return value, nil
	}
	if !strings.Contains(in, "${value.") {
		return in, nil
	}
	var resolveErr error
	out := embeddedRef.ReplaceAllStringFunc(in, func(match string) string {
		name := embeddedRef.FindStringSubmatch(match)[1]
		value, ok := lookup(vals, name)
		if !ok {
			resolveErr = unknown(name, vals)
			return match
		}
		switch typed := value.(type) {
		case string:
			return typed
		case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return fmt.Sprint(typed)
		default:
			resolveErr = fmt.Errorf("value %q is not a scalar and cannot be embedded", name)
			return match
		}
	})
	if resolveErr != nil {
		return nil, resolveErr
	}
	return out, nil
}

// ResolveStringSlice resolves each element, preserving element types.
func ResolveStringSlice(in []string, vals map[string]any) ([]string, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]string, len(in))
	for i, item := range in {
		resolved, err := ResolveString(item, vals)
		if err != nil {
			return nil, err
		}
		out[i] = resolved
	}
	return out, nil
}

func lookup(vals map[string]any, name string) (any, bool) {
	if value, ok := vals[name]; ok {
		return value, true
	}
	parts := strings.Split(name, ".")
	current := any(vals)
	for _, part := range parts {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func unknown(name string, vals map[string]any) error {
	keys := make([]string, 0, len(vals))
	for key := range vals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return fmt.Errorf("value %q is not defined (no values are configured)", name)
	}
	return fmt.Errorf("value %q is not defined (available: %s)", name, strings.Join(keys, ", "))
}
