// Package clitoken stores and retrieves the OIDC token obtained by
// `guardianctl login`, so CLI gRPC calls can present it as a bearer token.
package clitoken

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	coreauthz "github.com/radryc/monofs/pkg/authz"
)

// TokenPath returns the cache file location, honoring GUARDIANCTL_TOKEN_FILE,
// then XDG_CONFIG_HOME, then ~/.config/guardianctl/token.json.
func TokenPath() string {
	if p := strings.TrimSpace(os.Getenv("GUARDIANCTL_TOKEN_FILE")); p != "" {
		return p
	}
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ".guardianctl-token.json"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "guardianctl", "token.json")
}

// Save writes the token to the cache file.
func Save(tok *coreauthz.TokenResponse) error {
	return coreauthz.SaveTokenFile(TokenPath(), tok)
}

// Load reads the cached token, or returns (nil, error) when absent.
func Load() (*coreauthz.TokenResponse, error) {
	return coreauthz.LoadTokenFile(TokenPath())
}

// Delete removes the cached token (logout).
func Delete() error {
	err := os.Remove(TokenPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Bearer returns the token string to present to servers: the env override
// GUARDIANCTL_BEARER_TOKEN if set, otherwise the cached id_token (a JWT the
// servers can verify). Returns "" when no usable, unexpired token exists.
func Bearer() string {
	if t := strings.TrimSpace(os.Getenv("GUARDIANCTL_BEARER_TOKEN")); t != "" {
		return t
	}
	tok, err := Load()
	if err != nil || tok == nil {
		return ""
	}
	if tok.ExpiresIn > 0 && tok.ObtainedAt > 0 {
		if time.Now().Unix() >= tok.ObtainedAt+int64(tok.ExpiresIn) {
			return "" // expired
		}
	}
	if tok.IDToken != "" {
		return tok.IDToken
	}
	return tok.AccessToken
}
