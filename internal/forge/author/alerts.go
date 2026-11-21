package author

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/radryc/guardian/internal/bundle"
)

// SuggestAlerts generates a starter alert group for a partition from its
// declared probes. The result is deterministic and uses canonical metric label
// names, so display-key aliases never reach PromQL.
func SuggestAlerts(partitionDir string) ([]byte, error) {
	b, err := bundle.Load(partitionDir, bundle.Options{ResolveIntentRefs: bundle.ResolveImageRefs})
	if err != nil {
		return nil, err
	}
	pascal := pascalCase(b.PartitionName)

	var out strings.Builder
	out.WriteString("version: 1\n\n")
	out.WriteString("groups:\n")
	out.WriteString("  - name: probe-health\n")
	out.WriteString("    tenant: default\n")
	fmt.Fprintf(&out, "    partition: %s\n", b.PartitionName)
	out.WriteString("    interval: 30s\n")
	out.WriteString("    rules:\n")
	fmt.Fprintf(&out, "      - alert: %sProbeDown\n", pascal)
	fmt.Fprintf(&out, "        expr: min by (guardian_partition, guardian_asset, guardian_probe_name) (guardian_probe_up{guardian_partition=%q}) == 0\n", b.PartitionName)
	out.WriteString("        for: 2m\n")
	out.WriteString("        labels:\n")
	out.WriteString("          severity: critical\n")
	out.WriteString("        annotations:\n")
	fmt.Fprintf(&out, "          summary: \"A Guardian probe in the %s partition is down.\"\n", b.PartitionName)
	out.WriteString("          description: >-\n")
	fmt.Fprintf(&out, "            Guardian did not receive a successful probe for %s. Check the probes\n", b.PartitionName)
	out.WriteString("            page for the failing asset and target.\n")
	fmt.Fprintf(&out, "      - alert: %sProbeStale\n", pascal)
	fmt.Fprintf(&out, "        expr: time() - max by (guardian_partition, guardian_asset, guardian_probe_name) (guardian_probe_last_success_timestamp_seconds{guardian_partition=%q}) > 300\n", b.PartitionName)
	out.WriteString("        for: 2m\n")
	out.WriteString("        labels:\n")
	out.WriteString("          severity: warning\n")
	out.WriteString("        annotations:\n")
	fmt.Fprintf(&out, "          summary: \"A Guardian probe in the %s partition has not succeeded recently.\"\n", b.PartitionName)
	out.WriteString("          description: >-\n")
	out.WriteString("            The probe may be stuck or unable to reach its target. Check the Guardian\n")
	out.WriteString("            probes page.\n")
	return []byte(out.String()), nil
}

func pascalCase(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '.'
	})
	var out strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		out.WriteString(string(runes))
	}
	if out.Len() == 0 {
		return "Partition"
	}
	return out.String()
}
