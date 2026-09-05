package digest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func ContentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// NormalizedHash returns a deterministic hex hash of v marshaled to JSON,
// or an error if v cannot be marshaled.
func NormalizedHash(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return ContentHash(data), nil
}

// MustNormalizedHash is like NormalizedHash but panics on marshal failure.
// Use this only for inputs that are known to be JSON-serializable (e.g. plain
// structs with basic-type fields). Named after the Go convention for
// must-succeed wrappers such as regexp.MustCompile.
func MustNormalizedHash(v any) string {
	h, err := NormalizedHash(v)
	if err != nil {
		panic(fmt.Sprintf("MustNormalizedHash: json.Marshal failed: %v", err))
	}
	return h
}
