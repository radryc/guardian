package author

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/radryc/guardian/internal/access"
	"github.com/radryc/guardian/internal/compiler/validator"
	partitiondomain "github.com/radryc/guardian/internal/domain/partition"
	"gopkg.in/yaml.v3"
)

// PartitionAccessFileName is the repo-owned per-partition access policy.
const PartitionAccessFileName = access.PartitionPolicyFileName

// ConfigPath returns the partition manifest path, accepting the legacy
// partition.yaml name as well as config.yaml.
func ConfigPath(partitionDir string) (string, error) {
	for _, name := range []string{"config.yaml", "partition.yaml"} {
		path := filepath.Join(partitionDir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("no config.yaml or partition.yaml under %s", partitionDir)
}

// ReadPartition decodes the partition manifest.
func ReadPartition(partitionDir string) (*partitiondomain.Partition, string, error) {
	path, err := ConfigPath(partitionDir)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var part partitiondomain.Partition
	if err := yaml.Unmarshal(data, &part); err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", path, err)
	}
	return &part, path, nil
}

// SetQuota replaces spec.quota on the partition manifest, preserving the rest
// of the file's formatting. A nil quota removes the section.
func SetQuota(partitionDir string, quota any) (string, error) {
	path, err := ConfigPath(partitionDir)
	if err != nil {
		return "", err
	}
	doc, err := loadDocument(path)
	if err != nil {
		return "", err
	}
	root, err := rootMapping(doc)
	if err != nil {
		return "", err
	}
	spec, err := mappingChild(root, "spec", true)
	if err != nil {
		return "", err
	}
	if quota == nil {
		removeMappingKey(spec, "quota")
	} else {
		node, err := nodeFromAny(quota)
		if err != nil {
			return "", err
		}
		setMapping(spec, "quota", node)
	}
	return path, savePartition(path, doc)
}

// ReadPartitionAccess returns the partition access policy, if it exists.
func ReadPartitionAccess(partitionDir string) ([]byte, bool, error) {
	path := filepath.Join(partitionDir, PartitionAccessFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// SavePartitionAccess validates and writes the partition-scoped access policy.
// Validation enforces that the file only grants the owning partition and does
// not contain platform-owned sections such as quotas or selectors.
func SavePartitionAccess(partitionDir, partition, displayPath string, data []byte) (string, error) {
	if _, err := access.ValidatePartitionPolicy(data, partition, displayPath); err != nil {
		return "", err
	}
	path := filepath.Join(partitionDir, PartitionAccessFileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// PartitionAccessTemplate is a starter access.yaml for one partition.
func PartitionAccessTemplate(partition string) string {
	return fmt.Sprintf(`# Partition-scoped access policy authored by Forge.
# Grants may only name this partition; platform-owned sections such as
# defaults, selectors, quotas, and clients are rejected.
version: 1
users:
  user@example.com:
    partitions: [%[1]s]
    logs: [%[1]s]
    traces: [%[1]s]
    role: viewer
# groups:
#   platform-team:
#     partitions: [%[1]s]
#     logs: [%[1]s]
#     traces: [%[1]s]
#     role: viewer
`, partition)
}

func savePartition(path string, doc *yaml.Node) error {
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	var part partitiondomain.Partition
	if err := yaml.Unmarshal(data, &part); err != nil {
		return fmt.Errorf("parse updated partition: %w", err)
	}
	if err := validator.ValidatePartition(&part); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func removeMappingKey(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}
