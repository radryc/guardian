package cdkgen

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/radryc/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

// GlobalTemplateDir is the store namespace for global template overlays.
const GlobalTemplateDir = "/.templates/aws"

// PartitionTemplateDir returns the partition-scoped template overlay namespace.
func PartitionTemplateDir(partition string) string {
	return "/partitions/" + strings.Trim(partition, "/") + "/templates/aws"
}

// OverlayStore is the read-only store surface the overlay provider needs.
type OverlayStore interface {
	ReadFile(ctx context.Context, logicalPath string) ([]byte, error)
	ListDir(ctx context.Context, logicalDir string) ([]guardianapi.DirEntry, error)
}

// OverlayProvider resolves templates from a store overlay with precedence
// partition > global > embedded. Each overlay template directory carries a
// `template.yaml` catalog entry plus its module files; invalid entries are
// skipped with a warning and fall back to the next level.
type OverlayProvider struct {
	ctx       context.Context
	store     OverlayStore
	base      TemplateProvider
	partition string

	loaded      bool
	catalog     *Catalog
	overlayDirs map[string]overlayDir
	warnings    []string
}

type overlayDir struct {
	source string
	files  map[string][]byte
}

// NewOverlayProvider builds a provider that layers store overlays over base.
func NewOverlayProvider(ctx context.Context, store OverlayStore, partition string, base TemplateProvider) *OverlayProvider {
	if base == nil {
		base = EmbeddedProvider()
	}
	return &OverlayProvider{
		ctx:       ctx,
		store:     store,
		base:      base,
		partition: partition,
	}
}

// Warnings returns overlay problems observed during loading.
func (p *OverlayProvider) Warnings() []string { return p.warnings }

// Catalog returns the effective catalog (embedded + overlay entries).
func (p *OverlayProvider) Catalog() *Catalog {
	p.load()
	return p.catalog
}

// ResolveDir returns the winning file set for a template directory.
func (p *OverlayProvider) ResolveDir(dir string) (map[string][]byte, string, error) {
	p.load()
	if overlay, ok := p.overlayDirs[dir]; ok {
		return overlay.files, overlay.source, nil
	}
	return p.base.ResolveDir(dir)
}

func (p *OverlayProvider) load() {
	if p.loaded {
		return
	}
	p.loaded = true

	catalog, err := cloneCatalog(p.base.Catalog())
	if err != nil {
		p.warnings = append(p.warnings, fmt.Sprintf("clone base catalog: %v", err))
		catalog = &Catalog{Version: "unknown"}
	}
	p.overlayDirs = map[string]overlayDir{}

	// Global first so partition overlays win.
	levels := []struct {
		root   string
		source string
	}{
		{GlobalTemplateDir, TemplateSourceGlobal},
		{PartitionTemplateDir(p.partition), TemplateSourcePartition},
	}
	for _, level := range levels {
		if strings.TrimSpace(p.partition) == "" && level.source == TemplateSourcePartition {
			continue
		}
		p.loadLevel(catalog, level.root, level.source)
	}
	p.catalog = catalog
}

func (p *OverlayProvider) loadLevel(catalog *Catalog, root, source string) {
	dirs, err := p.listTemplateDirs(root)
	if err != nil {
		if !isNotExist(err) {
			p.warnings = append(p.warnings, fmt.Sprintf("list %s: %v", root, err))
		}
		return
	}
	for _, dir := range dirs {
		dirPath := root + "/" + dir
		files, err := p.readFiles(dirPath, "")
		if err != nil {
			p.warnings = append(p.warnings, fmt.Sprintf("read %s: %v", dirPath, err))
			continue
		}
		if len(files) == 0 {
			continue
		}
		if raw, ok := files["template.yaml"]; ok {
			entry, err := parseOverlayEntry(raw)
			if err != nil {
				p.warnings = append(p.warnings, fmt.Sprintf("template overlay %s: %v", dirPath, err))
				continue
			}
			replaceOrAppendTemplate(catalog, entry)
		}
		delete(files, "template.yaml")
		p.overlayDirs[dir] = overlayDir{source: source, files: files}
	}
}

func (p *OverlayProvider) listTemplateDirs(root string) ([]string, error) {
	entries, err := p.store.ListDir(p.ctx, root)
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir {
			dirs = append(dirs, entry.Name)
		}
	}
	return dirs, nil
}

func (p *OverlayProvider) readFiles(dirPath, prefix string) (map[string][]byte, error) {
	entries, err := p.store.ListDir(p.ctx, dirPath)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, entry := range entries {
		name := entry.Name
		rel := name
		if prefix != "" {
			rel = prefix + "/" + name
		}
		if entry.IsDir {
			nested, err := p.readFiles(dirPath+"/"+name, rel)
			if err != nil {
				return nil, err
			}
			for key, raw := range nested {
				files[key] = raw
			}
			continue
		}
		raw, err := p.store.ReadFile(p.ctx, dirPath+"/"+name)
		if err != nil {
			return nil, err
		}
		files[rel] = raw
	}
	return files, nil
}

func parseOverlayEntry(raw []byte) (*TemplateEntry, error) {
	var entry TemplateEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("parse template.yaml: %w", err)
	}
	if entry.ID == "" {
		return nil, fmt.Errorf("template.yaml: id is required")
	}
	if entry.Module == "" || entry.Export == "" {
		return nil, fmt.Errorf("template.yaml: module and export are required")
	}
	if len(entry.AssetTypes) == 0 && !entry.Implicit {
		return nil, fmt.Errorf("template.yaml: assetTypes is required unless implicit")
	}
	for _, rule := range entry.Match {
		switch strings.ToLower(rule.Op) {
		case "equals", "in", "contains", "exists", "notexists":
		default:
			return nil, fmt.Errorf("template.yaml: unsupported match op %q", rule.Op)
		}
	}
	return &entry, nil
}

func replaceOrAppendTemplate(catalog *Catalog, entry *TemplateEntry) {
	for i := range catalog.Templates {
		if catalog.Templates[i].ID == entry.ID {
			catalog.Templates[i] = *entry
			return
		}
	}
	catalog.Templates = append(catalog.Templates, *entry)
}

func cloneCatalog(base *Catalog) (*Catalog, error) {
	if base == nil {
		return nil, fmt.Errorf("base catalog is nil")
	}
	raw, err := yaml.Marshal(base)
	if err != nil {
		return nil, err
	}
	var catalog Catalog
	if err := yaml.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	return &catalog, nil
}

func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not found") || strings.Contains(message, "no such file")
}
