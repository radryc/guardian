package results

import (
	"os"
	"testing"
)

// TestMain keeps task-archive writes on the explicitly allowed unkeyed chain;
// production deployments must configure GUARDIAN_ARCHIVE_HMAC_KEY.
func TestMain(m *testing.M) {
	if os.Getenv("GUARDIAN_ARCHIVE_HMAC_KEY") == "" && os.Getenv("GUARDIAN_ALLOW_UNKEYED_ARCHIVE") == "" {
		_ = os.Setenv("GUARDIAN_ALLOW_UNKEYED_ARCHIVE", "true")
	}
	os.Exit(m.Run())
}
