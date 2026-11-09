package gcpdriver

import (
	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	probepkg "github.com/radryc/guardian/internal/pusher/drivers/probe"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/pusher/secrets"
)

type baseDriver struct {
	backend  BackendAPI
	resolver secrets.Resolver
}

// Register installs the GCP asset drivers.
func Register(reg *registry.Registry, backend BackendAPI, resolver secrets.Resolver) {
	if reg == nil || backend == nil {
		return
	}
	if resolver == nil {
		resolver = secrets.NoopResolver{}
	}
	base := baseDriver{backend: backend, resolver: resolver}
	reg.Register(&ObjectStoreDriver{base})
	reg.Register(&NetworkDriver{base})
	reg.Register(&SecretDriver{base})
	reg.Register(&ComputeDriver{base})
	reg.Register(&SQLDatabaseDriver{baseDriver: base, typeName: assetdomain.TypeDatabase})
	reg.Register(&SQLDatabaseDriver{baseDriver: base, typeName: assetdomain.TypeSQLDatabase})
	reg.Register(&VolumeDriver{base})
	reg.Register(&LoadBalancerDriver{base})
	reg.Register(&ObservabilityDriver{base})
	// Probe assets are executed by the pusher probe runner, not by a
	// deployment driver. Register the no-op driver so intents can mix Probe
	// assets with GCP resources.
	reg.Register(probepkg.New())
}
