package revisions

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
)

func PartitionRevision(configVersionID string, intentVersions map[string]string) string {
	keys := make([]string, 0, len(intentVersions))
	for key := range intentVersions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{configVersionID}
	for _, key := range keys {
		parts = append(parts, key+"="+intentVersions[key])
	}
	return "part_" + digest.ContentHash([]byte(strings.Join(parts, "|")))[:16]
}

func AssetVersionID(intentName string, assetSpec any) string {
	return "asset_" + digest.ContentHash([]byte(intentName + "|" + digest.MustNormalizedHash(assetSpec)))[:16]
}

func DerivedAssetVersion(assetVersionID string) string {
	return DerivedAssetVersionAt(assetVersionID, time.Time{})
}

func DerivedNamedAssetVersion(assetName, assetVersionID string) string {
	_ = assetName
	return DerivedAssetVersionAt(assetVersionID, time.Time{})
}

func DerivedAssetVersionAt(assetVersionID string, modifiedAt time.Time) string {
	trimmed := strings.TrimSpace(assetVersionID)
	if trimmed == "" {
		return ""
	}
	hashSuffix := strings.TrimPrefix(trimmed, "asset_")
	if hashSuffix == "" {
		return ""
	}
	base := "release-" + hashSuffix
	if modifiedAt.IsZero() {
		return base
	}
	return base + "-" + modifiedAt.UTC().Format("20060102-1504")
}

func DeploymentRevisionID(partitionRevision, intentVersionID string, t time.Time) string {
	payload := fmt.Sprintf("%s|%s|%s", partitionRevision, intentVersionID, t.UTC().Format(time.RFC3339Nano))
	return "dep_" + digest.ContentHash([]byte(payload))[:16]
}

func NewVersionID() string { return newID("ver") }

func NewTaskID() string { return newID("task") }

func NewEventID() string { return newID("evt") }

func NewCorrelationID() string { return newID("corr") }

func NewBatchRevisionID() string { return newID("batch") }

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UTC().UnixNano(), hex.EncodeToString(b[:]))
}
