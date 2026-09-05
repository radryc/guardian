package secrets

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type Resolver interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

type StoreResolver struct {
	Store guardianapi.ReadStore
}

func NewStoreResolver(store guardianapi.ReadStore) StoreResolver {
	return StoreResolver{Store: store}
}

type NoopResolver struct{}

func (NoopResolver) Resolve(ctx context.Context, ref string) (string, error) {
	return ref, nil
}

func (r StoreResolver) Resolve(ctx context.Context, ref string) (string, error) {
	if r.Store == nil {
		return "", fmt.Errorf("secret store is not configured")
	}
	logicalPath, err := secretLogicalPath(ref)
	if err != nil {
		return "", err
	}
	content, err := r.Store.ReadFile(ctx, logicalPath)
	if err != nil {
		return "", fmt.Errorf("read secret %s: %w", logicalPath, err)
	}
	value := strings.TrimRight(string(content), "\r\n")
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("secret %s is empty", logicalPath)
	}
	return value, nil
}

func secretLogicalPath(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("secret reference is required")
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("parse secret reference %q: %w", ref, err)
	}
	switch parsed.Scheme {
	case "monofs-secret":
		partition := strings.TrimSpace(parsed.Host)
		if partition == "" {
			return "", fmt.Errorf("monofs secret reference %q is missing partition", ref)
		}
		secretPath, err := cleanSecretPath(parsed.Path)
		if err != nil {
			return "", fmt.Errorf("monofs secret reference %q: %w", ref, err)
		}
		return "/partitions/" + partition + "/secrets/" + secretPath, nil
	default:
		if parsed.Scheme == "" {
			return "", fmt.Errorf("secret reference %q is missing a supported scheme", ref)
		}
		return "", fmt.Errorf("unsupported secret reference scheme %q", parsed.Scheme)
	}
}

func cleanSecretPath(raw string) (string, error) {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", fmt.Errorf("secret path is required")
	}
	parts := strings.Split(trimmed, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid secret path")
		}
		cleaned = append(cleaned, part)
	}
	return strings.Join(cleaned, "/"), nil
}

func ExtractSecretRef(value any) (ref string, isSecret bool) {
	switch typed := value.(type) {
	case map[string]any:
		raw, ok := typed["secret_ref"]
		if !ok {
			return "", false
		}
		ref, ok := raw.(string)
		return ref, ok
	case map[string]string:
		ref, ok := typed["secret_ref"]
		return ref, ok
	default:
		return "", false
	}
}

func ResolveString(ctx context.Context, resolver Resolver, value any) (string, error) {
	if resolver == nil {
		resolver = NoopResolver{}
	}
	if ref, isSecret := ExtractSecretRef(value); isSecret {
		return resolver.Resolve(ctx, ref)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case fmt.Stringer:
		return typed.String(), nil
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", typed), nil
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", typed), nil
	case float32, float64:
		return fmt.Sprintf("%v", typed), nil
	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("unsupported secret-backed string value type %T", value)
	}
}

func ResolveStringMap(ctx context.Context, resolver Resolver, values map[string]any) (map[string]string, error) {
	if len(values) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("environment key must not be empty")
		}
		resolved, err := ResolveString(ctx, resolver, value)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", key, err)
		}
		out[key] = resolved
	}
	return out, nil
}
