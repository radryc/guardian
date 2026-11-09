package gcpdriver

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	nonNameChar = regexp.MustCompile(`[^a-z0-9-]+`)
	dashRun     = regexp.MustCompile(`-+`)
)

// resourceName builds a deterministic, GCP-safe name from the asset
// coordinates. Long names are truncated and suffixed with a short hash so the
// result stays unique and within maxLen.
func resourceName(partition, intent, asset string, maxLen int) string {
	parts := []string{"guardian", partition, intent, asset}
	sanitized := make([]string, 0, len(parts))
	for _, part := range parts {
		if clean := sanitizeName(part); clean != "" {
			sanitized = append(sanitized, clean)
		}
	}
	name := strings.Join(sanitized, "-")
	if name == "" {
		name = "guardian-asset"
	}
	if maxLen <= 0 || len(name) <= maxLen {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(sum[:])[:10]
	return strings.TrimRight(name[:maxLen-len(suffix)-1], "-") + "-" + suffix
}

func sanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, ".", "-")
	value = nonNameChar.ReplaceAllString(value, "-")
	value = dashRun.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

// shortHash returns the first 63 characters of an asset hash. GCP label values
// are limited to 63 characters.
func shortHash(hash string) string {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if len(hash) <= 63 {
		return hash
	}
	return hash[:63]
}

func bucketName(partition, intent, asset string) string {
	return resourceName(partition, intent, asset, 63)
}

func networkName(partition, intent, asset string) string {
	return resourceName(partition, intent, asset, 63)
}

func subnetName(partition, intent, asset string) string {
	return resourceName(partition, intent, asset+"-subnet", 63)
}

func secretName(partition, intent, asset string) string {
	return resourceName(partition, intent, asset, 255)
}

// cloudRunName respects Cloud Run's 49-character service name limit.
func cloudRunName(partition, intent, asset string) string {
	return resourceName(partition, intent, asset, 49)
}

func sqlInstanceName(partition, intent, asset string) string {
	return resourceName(partition, intent, asset, 63)
}

func filestoreName(partition, intent, asset string) string {
	return resourceName(partition, intent, asset, 63)
}

// lbName is limited so backend, proxy, and forwarding-rule suffixes stay within
// the 63-character GCP name limit.
func lbName(partition, intent, asset string) string {
	return resourceName(partition, intent, asset, 40)
}

func otelGatewayName(partition, intent, asset string) string {
	return resourceName(partition, intent, asset, 49)
}
