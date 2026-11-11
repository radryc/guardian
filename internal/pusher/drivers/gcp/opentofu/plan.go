package opentofu

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// PlanResult is the normalized result of a tofu plan.
type PlanResult struct {
	// Drift reports whether the plan contains any changes (exit code 2).
	Drift  bool
	Add    int
	Change int
	Remove int
	Import int
	// Diagnostics holds error diagnostics emitted by tofu.
	Diagnostics []string
	// Raw is the raw JSON-lines output, kept for task logs.
	Raw []byte
}

// Summary renders a short human-readable change count.
func (r PlanResult) Summary() string {
	return fmt.Sprintf("%d to add, %d to change, %d to destroy", r.Add, r.Change, r.Remove)
}

// ParsePlan extracts changes and diagnostics from tofu's JSON-lines output.
// Unknown lines are ignored so a provider upgrade cannot break parsing.
func ParsePlan(raw []byte) PlanResult {
	result := PlanResult{Raw: append([]byte(nil), raw...)}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry struct {
			Type    string `json:"type"`
			Changes struct {
				Add    int `json:"add"`
				Change int `json:"change"`
				Remove int `json:"remove"`
				Import int `json:"import"`
			} `json:"changes"`
			Diagnostic struct {
				Severity string `json:"severity"`
				Summary  string `json:"summary"`
				Detail   string `json:"detail"`
			} `json:"diagnostic"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "change_summary":
			result.Add = entry.Changes.Add
			result.Change = entry.Changes.Change
			result.Remove = entry.Changes.Remove
			result.Import = entry.Changes.Import
		case "diagnostic":
			if !strings.EqualFold(entry.Diagnostic.Severity, "error") {
				continue
			}
			message := strings.TrimSpace(entry.Diagnostic.Summary)
			if detail := strings.TrimSpace(entry.Diagnostic.Detail); detail != "" {
				if message != "" {
					message += ": "
				}
				message += detail
			}
			if message != "" {
				result.Diagnostics = append(result.Diagnostics, message)
			}
		}
	}
	return result
}
