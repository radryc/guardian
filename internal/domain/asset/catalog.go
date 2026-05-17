package asset

const (
	TypeCompute       = "Compute"
	TypeCDKStack      = "CDKStack"
	TypeDatabase      = "Database"
	TypeSQLDatabase   = "SQLDatabase"
	TypeVolume        = "Volume"
	TypeConfig        = "Config"
	TypeNetwork       = "Network"
	TypeLoadBalancer  = "LoadBalancer"
	TypeObjectStore   = "ObjectStore"
	TypeObservability = "Observability"
)

var knownTypes = map[string]struct{}{
	TypeCompute:       {},
	TypeCDKStack:      {},
	TypeDatabase:      {},
	TypeSQLDatabase:   {},
	TypeVolume:        {},
	TypeConfig:        {},
	TypeNetwork:       {},
	TypeLoadBalancer:  {},
	TypeObjectStore:   {},
	TypeObservability: {},
}

func IsKnownType(assetType string) bool {
	_, ok := knownTypes[assetType]
	return ok
}

func KnownTypes() []string {
	return []string{
		TypeCompute,
		TypeCDKStack,
		TypeDatabase,
		TypeSQLDatabase,
		TypeVolume,
		TypeConfig,
		TypeNetwork,
		TypeLoadBalancer,
		TypeObjectStore,
		TypeObservability,
	}
}
