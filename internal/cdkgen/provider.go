package cdkgen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// TemplateProvider resolves the effective catalog and template file trees for
// one generation pass. The embedded provider is the always-available baseline;
// the overlay provider adds store-backed global/partition overrides.
type TemplateProvider interface {
	// Catalog returns the effective catalog.
	Catalog() *Catalog
	// ResolveDir returns the files of a template directory (for example
	// "network.vpc" or "_shared") keyed by path relative to the directory.
	ResolveDir(dir string) (map[string][]byte, string, error)
	// Warnings returns non-fatal problems observed while resolving overlays.
	Warnings() []string
}

type embeddedProvider struct {
	catalog *Catalog
}

// EmbeddedProvider returns the built-in template provider backed by the
// embedded aws/templates/aws/v1 tree.
func EmbeddedProvider() TemplateProvider {
	return &embeddedProvider{catalog: defaultCatalog}
}

func (p *embeddedProvider) Catalog() *Catalog { return p.catalog }

func (p *embeddedProvider) Warnings() []string { return nil }

func (p *embeddedProvider) ResolveDir(dir string) (map[string][]byte, string, error) {
	files, err := readDirFS(embeddedTemplates(), dir)
	if err != nil {
		return nil, "", err
	}
	return files, TemplateSourceEmbedded, nil
}

// Template source labels recorded in the payload manifest.
const (
	TemplateSourceEmbedded  = "embedded"
	TemplateSourceGlobal    = "global"
	TemplateSourcePartition = "partition"
)

func readDirFS(fsys fs.FS, dir string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(fsys, dir, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(filePath, dir), "/")
		raw, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return err
		}
		out[relative] = raw
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("template directory %q not found", dir)
	}
	return out, nil
}

// TemplateDigest computes the stable digest of a template file set. The digest
// is recorded in guardian.payload.json so a deployment can be traced back to
// the exact template revision that produced it.
func TemplateDigest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(files[name])
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// PolicyFileName is the per-template IAM declaration read from a template
// directory. It lists the actions the construct needs from the CloudFormation
// execution role.
const PolicyFileName = "policy.yaml"

type templatePolicy struct {
	Actions []string `yaml:"actions"`
}

// TemplateActions returns the IAM actions declared by one template directory
// (empty when the directory declares none).
func TemplateActions(provider TemplateProvider, dir string) []string {
	if provider == nil || strings.TrimSpace(dir) == "" {
		return nil
	}
	files, _, err := provider.ResolveDir(dir)
	if err != nil {
		return nil
	}
	raw, ok := files[PolicyFileName]
	if !ok {
		return nil
	}
	var policy templatePolicy
	if err := yaml.Unmarshal(raw, &policy); err != nil {
		return nil
	}
	actions := make([]string, 0, len(policy.Actions))
	seen := map[string]bool{}
	for _, action := range policy.Actions {
		action = strings.TrimSpace(action)
		if action == "" || seen[action] {
			continue
		}
		seen[action] = true
		actions = append(actions, action)
	}
	sort.Strings(actions)
	return actions
}

// PlatformExecutionActions are required by every generated stack regardless of
// which templates it uses: the CDK bootstrap version parameter (SSM) and the
// CloudFormation reads the service performs while creating the stack.
var PlatformExecutionActions = []string{
	"cloudformation:DescribeStackResources",
	"cloudformation:DescribeStacks",
	"cloudformation:GetTemplate",
	"cloudformation:ListStackResources",
	"ssm:GetParameter",
	"ssm:GetParameters",
	"ssm:GetParametersByPath",
}

// CatalogActions returns the union of every catalog template's declared
// actions, including the implicit templates. This is what `aws init` renders
// the account execution policies from, so the policies always follow the
// catalog.
func CatalogActions(provider TemplateProvider) []string {
	if provider == nil {
		provider = EmbeddedProvider()
	}
	seen := map[string]bool{}
	dirs := map[string]bool{}
	for _, entry := range provider.Catalog().Templates {
		dirs[entry.Dir()] = true
	}
	for dir := range dirs {
		for _, action := range TemplateActions(provider, dir) {
			seen[action] = true
		}
	}
	actions := make([]string, 0, len(seen))
	for action := range seen {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	return actions
}
