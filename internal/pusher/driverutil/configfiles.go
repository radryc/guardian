package driverutil

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	"github.com/radryc/guardian/internal/pusher/registry"
	"gopkg.in/yaml.v3"
)

// ConfigFileSet is the resolved content of a Config asset. Files are keyed by
// the filename rendered by the target pusher (ConfigMap key, docker config
// file, SSM parameter document name, ...). Binary marks content that must be
// stored as binary data by pushers that support it.
type ConfigFileSet struct {
	Files  map[string]string
	Binary bool
}

// Universal Config payload keys. They are deliberately not named after a
// pusher so the same asset can target Kubernetes, Docker, or AWS.
const (
	ConfigPayloadFile  = "file"
	ConfigPayloadRaw   = "raw"
	ConfigPayloadFiles = "files"
)

// structuredConfigPayload is the multi-file payload document. Both spellings
// are accepted: files: is the canonical name, data: is kept for compatibility
// with existing Kubernetes Config payloads.
type structuredConfigPayload struct {
	Files map[string]string `yaml:"files,omitempty"`
	Data  map[string]string `yaml:"data,omitempty"`
}

// NormalizeConfigFormat is re-exported for callers that resolve Config content
// without decoding the asset spec.
func NormalizeConfigFormat(format string) string {
	return assetdomain.NormalizeConfigFormat(format)
}

// IsBinaryConfig reports whether the asset content must be treated as binary.
func IsBinaryConfig(spec *assetdefs.ConfigSpec) bool {
	return spec != nil && assetdomain.IsBinaryConfigFormat(spec.Format)
}

// ResolveConfigFiles resolves a Config asset to its rendered files without
// assuming a specific pusher.
//
// Resolution order:
//  1. payload.file / payload.raw — a single raw file. The key is the payload
//     file's basename and the file content is used verbatim.
//  2. payload.files — a structured YAML document with a files: (or data:) map.
//  3. the pusher's legacy keys, in order (k8s/kubernetes, docker, aws, ...).
//  4. properties.content / properties.data.
//
// Binary content from properties or a structured payload is base64-encoded;
// payload.file always carries raw bytes.
func ResolveConfigFiles(ctx context.Context, in registry.AssetInput, providerKeys []string, spec *assetdefs.ConfigSpec) (ConfigFileSet, error) {
	binary := IsBinaryConfig(spec)

	rawKey, logicalPath, err := universalRawPayload(in.Asset.Payload)
	if err != nil {
		return ConfigFileSet{}, fmt.Errorf("asset %q (%s): %w", in.Asset.Name, in.Asset.Type, err)
	}
	if logicalPath != "" {
		content, err := readConfigPayload(ctx, in, logicalPath)
		if err != nil {
			return ConfigFileSet{}, err
		}
		name := path.Base(logicalPath)
		if err := validateConfigFileName(name); err != nil {
			return ConfigFileSet{}, fmt.Errorf("asset %q payload.%s: %w", in.Asset.Name, rawKey, err)
		}
		if !binary && !utf8.Valid(content) {
			binary = true
		}
		return ConfigFileSet{
			Files:  map[string]string{name: string(content)},
			Binary: binary,
		}, nil
	}

	if logicalPath := strings.TrimSpace(in.Asset.Payload[ConfigPayloadFiles]); logicalPath != "" {
		files, err := readStructuredConfigPayload(ctx, in, logicalPath, binary)
		if err != nil {
			return ConfigFileSet{}, err
		}
		return ConfigFileSet{Files: files, Binary: binary}, nil
	}

	for _, key := range providerKeys {
		logicalPath := strings.TrimSpace(in.Asset.Payload[key])
		if logicalPath == "" {
			continue
		}
		files, err := readStructuredConfigPayload(ctx, in, logicalPath, binary)
		if err != nil {
			return ConfigFileSet{}, err
		}
		return ConfigFileSet{Files: files, Binary: binary}, nil
	}

	files := ConfigFiles(spec)
	if binary {
		decoded, err := decodeBase64Files(files)
		if err != nil {
			return ConfigFileSet{}, fmt.Errorf("asset %q binary properties: %w", in.Asset.Name, err)
		}
		files = decoded
	}
	return ConfigFileSet{Files: files, Binary: binary}, nil
}

// HashContent returns a JSON-safe representation of the file set for hashing.
// Binary content is base64-encoded so invalid UTF-8 cannot collapse distinct
// payloads to the same hash.
func (s ConfigFileSet) HashContent() map[string]string {
	if !s.Binary {
		return s.Files
	}
	out := make(map[string]string, len(s.Files))
	for name, content := range s.Files {
		out[name] = base64.StdEncoding.EncodeToString([]byte(content))
	}
	return out
}

// HashStruct returns a stable, JSON-safe value for hashing a file set.
func (s ConfigFileSet) HashStruct() any {
	return struct {
		Files  map[string]string
		Binary bool
	}{
		Files:  s.HashContent(),
		Binary: s.Binary,
	}
}

// ResolveNamedConfigFiles resolves a Config asset referenced by another asset.
func ResolveNamedConfigFiles(ctx context.Context, in registry.AssetInput, name string, providerKeys []string) (ConfigFileSet, *assetdefs.ConfigSpec, error) {
	asset, typed, err := DecodeNamedAsset(in, name)
	if err != nil {
		return ConfigFileSet{}, nil, err
	}
	spec, ok := typed.(*assetdefs.ConfigSpec)
	if !ok {
		return ConfigFileSet{}, nil, fmt.Errorf("referenced asset %q is %s, not Config", name, asset.Type)
	}
	sub := in
	sub.Asset = asset
	files, err := ResolveConfigFiles(ctx, sub, providerKeys, spec)
	return files, spec, err
}

// SingleConfigFileName returns the filename when the set contains exactly one
// file.
func SingleConfigFileName(files map[string]string) (string, bool) {
	if len(files) != 1 {
		return "", false
	}
	for name := range files {
		return name, true
	}
	return "", false
}

// ConfigFileContent returns the single file's content when the set contains
// exactly one file.
func ConfigFileContent(files map[string]string) (string, string, bool) {
	if len(files) != 1 {
		return "", "", false
	}
	for name, content := range files {
		return name, content, true
	}
	return "", "", false
}

func universalRawPayload(payload map[string]string) (string, string, error) {
	filePath := strings.TrimSpace(payload[ConfigPayloadFile])
	rawPath := strings.TrimSpace(payload[ConfigPayloadRaw])
	if filePath != "" && rawPath != "" {
		return "", "", fmt.Errorf("payload.file and payload.raw are mutually exclusive")
	}
	chosenKey := ConfigPayloadFile
	chosenPath := filePath
	if chosenPath == "" {
		chosenKey = ConfigPayloadRaw
		chosenPath = rawPath
	}
	if chosenPath == "" {
		return "", "", nil
	}
	if structured := strings.TrimSpace(payload[ConfigPayloadFiles]); structured != "" {
		return "", "", fmt.Errorf("payload.%s and payload.%s are mutually exclusive", chosenKey, ConfigPayloadFiles)
	}
	return chosenKey, chosenPath, nil
}

func readConfigPayload(ctx context.Context, in registry.AssetInput, logicalPath string) ([]byte, error) {
	if in.Store == nil {
		return nil, fmt.Errorf("asset %q payload %s cannot be read without a store", in.Asset.Name, logicalPath)
	}
	content, err := in.Store.ReadFile(ctx, logicalPath)
	if err != nil {
		return nil, fmt.Errorf("read payload %s: %w", logicalPath, err)
	}
	return content, nil
}

func readStructuredConfigPayload(ctx context.Context, in registry.AssetInput, logicalPath string, binary bool) (map[string]string, error) {
	content, err := readConfigPayload(ctx, in, logicalPath)
	if err != nil {
		return nil, err
	}
	var payload structuredConfigPayload
	if err := yaml.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("decode config payload %s: %w", logicalPath, err)
	}
	if len(payload.Files) > 0 && len(payload.Data) > 0 {
		return nil, fmt.Errorf("config payload %s must use either files: or data:, not both", logicalPath)
	}
	files := payload.Files
	if len(files) == 0 {
		files = payload.Data
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("config payload %s must define a non-empty files: or data: map", logicalPath)
	}
	for name := range files {
		if err := validateConfigFileName(name); err != nil {
			return nil, fmt.Errorf("config payload %s: %w", logicalPath, err)
		}
	}
	if binary {
		files, err = decodeBase64Files(files)
		if err != nil {
			return nil, fmt.Errorf("config payload %s: %w", logicalPath, err)
		}
	}
	return files, nil
}

func decodeBase64Files(files map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(files))
	for name, content := range files {
		cleaned := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, content)
		decoded, err := base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			return nil, fmt.Errorf("file %q is not valid base64: %w", name, err)
		}
		out[name] = string(decoded)
	}
	return out, nil
}

func validateConfigFileName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("file name must not be empty")
	}
	if trimmed != name {
		return fmt.Errorf("file name %q must not have leading or trailing whitespace", name)
	}
	if strings.ContainsAny(trimmed, `/\`) || trimmed == "." || trimmed == ".." {
		return fmt.Errorf("file name %q must be a base filename", name)
	}
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("file name %q contains unsupported character %q", name, r)
	}
	return nil
}
