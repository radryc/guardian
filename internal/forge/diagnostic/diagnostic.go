// Package diagnostic defines the stable, machine-readable validation output
// used by every Forge command. Codes are grouped by domain:
//
//	FGE1xxx  schema and syntax
//	FGE2xxx  partition and intent semantics
//	FGE3xxx  cross-reference and graph
//	FGE4xxx  policy and guardrails
//	FGE5xxx  alerts
//	FGE6xxx  security and secrets
//	FGE7xxx  environment, platform, and pipeline
//	FGE8xxx  display keys and profiles
package diagnostic

import "strings"

// Severity is the diagnostic level.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Diagnostic is one validation finding.
type Diagnostic struct {
	Severity  Severity `json:"severity" yaml:"severity"`
	Code      string   `json:"code" yaml:"code"`
	Message   string   `json:"message" yaml:"message"`
	Partition string   `json:"partition,omitempty" yaml:"partition,omitempty"`
	File      string   `json:"file,omitempty" yaml:"file,omitempty"`
	Path      string   `json:"path,omitempty" yaml:"path,omitempty"`
	Hint      string   `json:"hint,omitempty" yaml:"hint,omitempty"`
}

// Error implements error so a diagnostic can be returned directly.
func (d Diagnostic) Error() string {
	var b strings.Builder
	b.WriteString(string(d.Severity))
	b.WriteString(" ")
	b.WriteString(d.Code)
	if d.File != "" {
		b.WriteString(" ")
		b.WriteString(d.File)
	}
	b.WriteString(": ")
	b.WriteString(d.Message)
	return b.String()
}

// List is an ordered diagnostic collection.
type List []Diagnostic

// Add appends a diagnostic.
func (l *List) Add(d Diagnostic) { *l = append(*l, d) }

// Errors returns the number of error-severity diagnostics.
func (l List) Errors() int {
	n := 0
	for _, d := range l {
		if d.Severity == SeverityError {
			n++
		}
	}
	return n
}

// Warnings returns the number of warning-severity diagnostics.
func (l List) Warnings() int {
	n := 0
	for _, d := range l {
		if d.Severity == SeverityWarning {
			n++
		}
	}
	return n
}

// HasErrors reports whether any error-severity diagnostic is present.
func (l List) HasErrors() bool { return l.Errors() > 0 }
