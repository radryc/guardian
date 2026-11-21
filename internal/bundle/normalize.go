package bundle

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/radryc/guardian/internal/compiler/manifest"
	"github.com/radryc/guardian/internal/compiler/validator"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	partitiondomain "github.com/radryc/guardian/internal/domain/partition"
	"gopkg.in/yaml.v3"
)

var yamlLinePattern = regexp.MustCompile(`line (\d+):`)

// NormalizePartitionManifest decodes, defaults, re-encodes, and validates one
// partition manifest. The returned bytes are the canonical form written to the
// store.
func NormalizePartitionManifest(raw []byte, sourcePath string) (*partitiondomain.Partition, []byte, error) {
	var part partitiondomain.Partition
	if err := yaml.Unmarshal(raw, &part); err != nil {
		return nil, nil, annotateYAMLManifestError("partition", sourcePath, raw, err)
	}
	part.APIVersion = coalesceString(part.APIVersion, "guardian/v1alpha1")
	part.Kind = coalesceString(part.Kind, "Partition")
	normalized, err := yaml.Marshal(part)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := manifest.ParsePartition(normalized)
	if err != nil {
		return nil, nil, fmt.Errorf("partition manifest %s: %w", sourcePath, err)
	}
	if err := validator.ValidatePartition(parsed); err != nil {
		return nil, nil, fmt.Errorf("partition manifest %s: %w", sourcePath, err)
	}
	return parsed, normalized, nil
}

// NormalizeIntentManifest decodes, defaults, re-encodes, and validates one
// intent manifest. A single-element YAML list is accepted for backwards
// compatibility. When knownIntents is non-nil, joins and intent references are
// validated against it.
func NormalizeIntentManifest(raw []byte, sourcePath string, knownIntents []string) (*intentdomain.Intent, []byte, error) {
	var intent intentdomain.Intent
	if err := yaml.Unmarshal(raw, &intent); err != nil {
		if !strings.Contains(err.Error(), "!!seq") {
			return nil, nil, annotateYAMLManifestError("intent", sourcePath, raw, err)
		}
		var docs []intentdomain.Intent
		if listErr := yaml.Unmarshal(raw, &docs); listErr != nil {
			return nil, nil, annotateYAMLManifestError("intent", sourcePath, raw, err)
		}
		if len(docs) == 0 {
			return nil, nil, fmt.Errorf("intent file %s is an empty YAML list", sourcePath)
		}
		if len(docs) > 1 {
			return nil, nil, fmt.Errorf("intent file %s contains %d YAML documents; split into separate files", sourcePath, len(docs))
		}
		intent = docs[0]
	}
	intent.APIVersion = coalesceString(intent.APIVersion, "guardian/v1alpha1")
	intent.Kind = coalesceString(intent.Kind, "Intent")
	normalized, err := yaml.Marshal(intent)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := manifest.ParseIntent(normalized)
	if err != nil {
		return nil, nil, fmt.Errorf("intent manifest %s: %w", sourcePath, err)
	}
	if knownIntents != nil {
		if err := validator.ValidateIntent(parsed, knownIntents, nil); err != nil {
			return nil, nil, fmt.Errorf("intent manifest %s: %w", sourcePath, err)
		}
	}
	normalized, err = yaml.Marshal(parsed)
	if err != nil {
		return nil, nil, err
	}
	return parsed, normalized, nil
}

func annotateYAMLManifestError(kind, sourcePath string, raw []byte, err error) error {
	message := fmt.Sprintf("parse %s %s: %v", kind, sourcePath, err)
	lineNumber := extractYAMLLineNumber(err)
	if lineNumber <= 0 {
		return errors.New(message)
	}
	lineText, ok := sourceLine(raw, lineNumber)
	if !ok {
		return errors.New(message)
	}
	return fmt.Errorf("%s\n  %d | %s", message, lineNumber, lineText)
}

func extractYAMLLineNumber(err error) int {
	match := yamlLinePattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return 0
	}
	lineNumber, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		return 0
	}
	return lineNumber
}

func sourceLine(raw []byte, lineNumber int) (string, bool) {
	if lineNumber <= 0 {
		return "", false
	}
	lines := bytes.Split(raw, []byte("\n"))
	if lineNumber > len(lines) {
		return "", false
	}
	return strings.TrimRight(string(lines[lineNumber-1]), "\r"), true
}

func coalesceString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
