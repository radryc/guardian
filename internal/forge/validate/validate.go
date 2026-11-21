// Package validate runs Forge's layered validation over a repository's
// partition directories.
//
// It composes internal/bundle (syntax, schema, semantics, typed assets,
// payload references) and adds Forge-specific checks: v1 asset coverage,
// unresolved image templates, and partition alert rules.
package validate

import (
	"context"
	"fmt"
	"strings"

	"github.com/radryc/guardian/internal/bundle"
	"github.com/radryc/guardian/internal/forge/diagnostic"
	"github.com/radryc/guardian/internal/forge/workspace"
	"github.com/radryc/guardian/internal/paths"
	"gopkg.in/yaml.v3"
)

// Options controls one validation run.
type Options struct {
	// Root is a repository root, a partitions directory, or one partition.
	Root string
	// Strict promotes warnings to a failing result.
	Strict bool
	// IncludeSecrets validates files under secrets/.
	IncludeSecrets bool
}

// Result is the machine-readable outcome.
type Result struct {
	Success           bool                    `json:"success"`
	Root              string                  `json:"root"`
	PartitionsChecked int                     `json:"partitionsChecked"`
	Errors            int                     `json:"errors"`
	Warnings          int                     `json:"warnings"`
	Diagnostics       []diagnostic.Diagnostic `json:"diagnostics,omitempty"`
}

// v1AssetTypes is the editable surface Forge guarantees for Kubernetes and
// Docker. Known types outside this set are reported as warnings so the tool can
// run over a mixed repository (for example AWS partitions) without failing.
var v1AssetTypes = map[string]bool{
	"Compute":       true,
	"ImageBuild":    true,
	"Config":        true,
	"Secret":        true,
	"Volume":        true,
	"Probe":         true,
	"Observability": true,
	"K8sResource":   true,
}

// Run validates every discovered partition under opts.Root.
func Run(ctx context.Context, opts Options) (Result, error) {
	result := Result{Root: opts.Root}
	refs, err := workspace.Discover(opts.Root)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, diagnostic.Diagnostic{
			Severity: diagnostic.SeverityError,
			Code:     "FGE7002",
			Message:  err.Error(),
			Hint:     "point --root at a repository, a partitions directory, or one partition",
		})
		return finish(result, opts.Strict)
	}
	result.PartitionsChecked = len(refs)

	var out diagnostic.List
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		b, err := bundle.Load(ref.Dir, bundle.Options{
			IncludeSecrets:    opts.IncludeSecrets,
			ResolveIntentRefs: bundle.ResolveImageRefs,
		})
		if err != nil {
			out.Add(classifyLoadError(ref, err))
			continue
		}
		out = append(out, checkV1Coverage(b)...)
		out = append(out, checkUnresolvedRefs(b)...)
		out = append(out, checkAlerts(b)...)
	}
	result.Diagnostics = out
	return finish(result, opts.Strict)
}

func finish(result Result, strict bool) (Result, error) {
	result.Errors = 0
	result.Warnings = 0
	for _, d := range result.Diagnostics {
		switch d.Severity {
		case diagnostic.SeverityError:
			result.Errors++
		case diagnostic.SeverityWarning:
			result.Warnings++
		}
	}
	result.Success = result.Errors == 0 && (!strict || result.Warnings == 0)
	if result.Success {
		return result, nil
	}
	return result, fmt.Errorf("%d error(s), %d warning(s)", result.Errors, result.Warnings)
}

func classifyLoadError(ref workspace.PartitionRef, err error) diagnostic.Diagnostic {
	message := err.Error()
	d := diagnostic.Diagnostic{
		Severity:  diagnostic.SeverityError,
		Code:      "FGE1000",
		Message:   message,
		Partition: ref.Name,
	}
	switch {
	case strings.Contains(message, "parse partition") || strings.Contains(message, "parse intent"):
		d.Code = "FGE1001"
		d.Hint = "fix the YAML syntax at the reported line"
	case strings.Contains(message, "duplicate intent metadata.name"):
		d.Code = "FGE2005"
		d.Hint = "intent metadata.name must be unique within a partition"
	case strings.Contains(message, "references missing file"):
		d.Code = "FGE3003"
		d.Hint = "add the referenced payload file or correct the logical path"
	case strings.Contains(message, "has unsupported type"):
		d.Code = "FGE3004"
		d.Hint = "use an asset type known to the Guardian asset registry"
	case strings.Contains(message, "invalid partition name") ||
		strings.Contains(message, "invalid intent name") ||
		strings.Contains(message, "invalid asset name"):
		d.Code = "FGE2001"
		d.Hint = "names must match ^[a-z][a-z0-9-]*$"
	case strings.Contains(message, "target."):
		d.Code = "FGE2002"
		d.Hint = "set target placement required by the pusher"
	default:
		d.Hint = "fix the reported manifest error"
	}
	return d
}

func checkV1Coverage(b *bundle.Bundle) diagnostic.List {
	var out diagnostic.List
	for _, intent := range b.IntentManifests {
		for _, asset := range intent.Spec.Assets {
			if v1AssetTypes[asset.Type] {
				continue
			}
			code := "FGE7001"
			msg := fmt.Sprintf("asset %q type %q is known but not editable in Forge v1", asset.Name, asset.Type)
			if !assetTypeKnown(asset.Type) {
				code = "FGE3004"
				msg = fmt.Sprintf("asset %q has unknown type %q", asset.Name, asset.Type)
			}
			out.Add(diagnostic.Diagnostic{
				Severity:  diagnostic.SeverityWarning,
				Code:      code,
				Message:   msg,
				Partition: b.PartitionName,
				Path:      fmt.Sprintf("intent/%s/assets/%s", intent.Metadata.Name, asset.Name),
				Hint:      "read-only in v1; keep authoring it by hand or with a later Forge release",
			})
		}
	}
	return out
}

// assetTypeKnown is a light registry probe. Unknown types are already rejected
// by bundle.Load, so this mainly distinguishes "known but out of v1 scope".
func assetTypeKnown(assetType string) bool {
	switch assetType {
	case "Database", "SQLDatabase", "ObjectStore", "Network", "LoadBalancer",
		"TraefikRoute", "CDKStack", "AWSResource":
		return true
	default:
		return false
	}
}

func checkUnresolvedRefs(b *bundle.Bundle) diagnostic.List {
	var out diagnostic.List
	for intentName, content := range b.IntentContents {
		if !strings.Contains(string(content), "${intent.") {
			continue
		}
		out.Add(diagnostic.Diagnostic{
			Severity:  diagnostic.SeverityWarning,
			Code:      "FGE3010",
			Message:   fmt.Sprintf("intent %q contains unresolved ${intent...} output references", intentName),
			Partition: b.PartitionName,
			Path:      fmt.Sprintf("intent/%s", intentName),
			Hint:      "provide .state/images.json or resolve the reference before release",
		})
	}
	return out
}

type alertDocument struct {
	Version int          `yaml:"version"`
	Groups  []alertGroup `yaml:"groups"`
}

type alertGroup struct {
	Name      string      `yaml:"name"`
	Tenant    string      `yaml:"tenant"`
	Partition string      `yaml:"partition"`
	Interval  string      `yaml:"interval"`
	Rules     []alertRule `yaml:"rules"`
}

type alertRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

func checkAlerts(b *bundle.Bundle) diagnostic.List {
	var out diagnostic.List
	prefix := paths.PartitionRoot(b.PartitionName) + "/alerts/"
	for logicalPath, content := range b.ManagedFiles {
		if !strings.HasPrefix(logicalPath, prefix) {
			continue
		}
		var doc alertDocument
		if err := yaml.Unmarshal(content, &doc); err != nil {
			out.Add(diagnostic.Diagnostic{
				Severity:  diagnostic.SeverityError,
				Code:      "FGE5001",
				Message:   fmt.Sprintf("alert file does not parse: %v", err),
				Partition: b.PartitionName,
				File:      logicalPath,
				Hint:      "alert files are YAML with version and groups[]",
			})
			continue
		}
		if doc.Version != 1 {
			out.Add(diagnostic.Diagnostic{
				Severity:  diagnostic.SeverityWarning,
				Code:      "FGE5005",
				Message:   fmt.Sprintf("alert file version is %d, expected 1", doc.Version),
				Partition: b.PartitionName,
				File:      logicalPath,
			})
		}
		for i, group := range doc.Groups {
			groupPath := fmt.Sprintf("%s#groups[%d]", logicalPath, i)
			if strings.TrimSpace(group.Name) == "" {
				out.Add(diagnostic.Diagnostic{
					Severity:  diagnostic.SeverityError,
					Code:      "FGE5003",
					Message:   "alert group name is required",
					Partition: b.PartitionName,
					File:      groupPath,
				})
			}
			if strings.TrimSpace(group.Partition) != "" && group.Partition != b.PartitionName {
				out.Add(diagnostic.Diagnostic{
					Severity:  diagnostic.SeverityError,
					Code:      "FGE5002",
					Message:   fmt.Sprintf("alert group %q partition %q does not match %q", group.Name, group.Partition, b.PartitionName),
					Partition: b.PartitionName,
					File:      groupPath,
					Hint:      "the partition label must match the directory name",
				})
			}
			for j, rule := range group.Rules {
				rulePath := fmt.Sprintf("%s#groups[%d].rules[%d]", logicalPath, i, j)
				if strings.TrimSpace(rule.Alert) == "" {
					out.Add(diagnostic.Diagnostic{
						Severity:  diagnostic.SeverityError,
						Code:      "FGE5003",
						Message:   "alert name is required",
						Partition: b.PartitionName,
						File:      rulePath,
					})
				}
				if strings.TrimSpace(rule.Expr) == "" {
					out.Add(diagnostic.Diagnostic{
						Severity:  diagnostic.SeverityError,
						Code:      "FGE5003",
						Message:   fmt.Sprintf("alert %q expression is required", rule.Alert),
						Partition: b.PartitionName,
						File:      rulePath,
					})
				}
				if strings.TrimSpace(rule.Annotations["summary"]) == "" {
					out.Add(diagnostic.Diagnostic{
						Severity:  diagnostic.SeverityWarning,
						Code:      "FGE5004",
						Message:   fmt.Sprintf("alert %q has no annotations.summary", rule.Alert),
						Partition: b.PartitionName,
						File:      rulePath,
					})
				}
			}
		}
	}
	return out
}
