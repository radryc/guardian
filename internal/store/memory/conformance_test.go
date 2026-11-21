package memory

import (
	"testing"

	"github.com/radryc/guardian/internal/store/storetest"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func TestStoreConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) guardianapi.Store {
		t.Helper()
		return New()
	})
}
