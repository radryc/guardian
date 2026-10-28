package asset

import "strings"

// NormalizeConfigFormat canonicalizes the Config asset format field.
func NormalizeConfigFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text", "txt":
		return "text"
	case "yaml", "yml":
		return "yaml"
	case "json":
		return "json"
	case "binary", "bin":
		return "binary"
	case "toml":
		return "toml"
	case "ini":
		return "ini"
	case "cfg", "conf":
		return "cfg"
	default:
		return strings.ToLower(strings.TrimSpace(format))
	}
}

// IsBinaryConfigFormat reports whether a Config format carries binary bytes.
func IsBinaryConfigFormat(format string) bool {
	return NormalizeConfigFormat(format) == "binary"
}
