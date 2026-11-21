package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// intentOutputRef matches ${intent.<name>.outputs.<asset>.<field>}.
var intentOutputRef = regexp.MustCompile(`\$\{intent\.[^.]+\.outputs\.([^.]+)\.([^}]+)\}`)

// imageState is the subset of <partition>/.state/images.json needed to resolve
// cross-intent output references. Unknown fields are ignored.
type imageState struct {
	Images map[string]imageEntry `json:"images"`
}

type imageEntry struct {
	ImageRef   string `json:"imageRef"`
	LocalTag   string `json:"localTag"`
	Repository string `json:"repository"`
	Registry   string `json:"registry"`
	Tag        string `json:"tag"`
}

// ResolveImageRefs rewrites ${intent.NAME.outputs.ASSET.FIELD} placeholders in
// the given intent contents using <dir>/.state/images.json, in memory.
//
// It is a no-op when the state file is absent or empty, so a partition can be
// validated before any image has been built. Disk files are never modified.
func ResolveImageRefs(dir string, files []IntentFile) error {
	data, err := os.ReadFile(filepath.Join(dir, ".state", "images.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load image state: %w", err)
	}
	var state imageState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode image state: %w", err)
	}
	if len(state.Images) == 0 {
		return nil
	}
	for i := range files {
		var doc yaml.Node
		if err := yaml.Unmarshal(files[i].Content, &doc); err != nil {
			return fmt.Errorf("parse intent %s: %w", files[i].Path, err)
		}
		if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
			continue
		}
		if stampImageRefs(doc.Content[0], state) {
			out, err := yaml.Marshal(&doc)
			if err != nil {
				return fmt.Errorf("marshal intent %s: %w", files[i].Path, err)
			}
			files[i].Content = out
		}
	}
	return nil
}

func stampImageRefs(node *yaml.Node, state imageState) bool {
	if node == nil || len(state.Images) == 0 {
		return false
	}
	modified := false
	walkImageRefs(node, state, &modified)
	return modified
}

func walkImageRefs(node *yaml.Node, state imageState, modified *bool) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.ScalarNode:
		resolved := intentOutputRef.ReplaceAllStringFunc(node.Value, func(match string) string {
			groups := intentOutputRef.FindStringSubmatch(match)
			if len(groups) != 3 {
				return match
			}
			entry, ok := state.Images[groups[1]]
			if !ok {
				return match
			}
			switch groups[2] {
			case "imageRef":
				if entry.ImageRef != "" {
					return entry.ImageRef
				}
			case "localTag":
				if entry.LocalTag != "" {
					return entry.LocalTag
				}
			case "repository":
				return entry.Repository
			case "registry":
				return entry.Registry
			case "tag":
				if entry.Tag != "" {
					return entry.Tag
				}
			}
			return match
		})
		if resolved != node.Value {
			node.Value = resolved
			*modified = true
		}
	case yaml.MappingNode, yaml.SequenceNode:
		for _, child := range node.Content {
			walkImageRefs(child, state, modified)
		}
	}
}
