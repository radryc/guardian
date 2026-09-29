package asset

const (
	TypeCompute       = "Compute"
	TypeImageBuild    = "ImageBuild"
	TypeCDKStack      = "CDKStack"
	TypeDatabase      = "Database"
	TypeSQLDatabase   = "SQLDatabase"
	TypeVolume        = "Volume"
	TypeConfig        = "Config"
	TypeNetwork       = "Network"
	TypeSecret        = "Secret"
	TypeLoadBalancer  = "LoadBalancer"
	TypeTraefikRoute  = "TraefikRoute"
	TypeObjectStore   = "ObjectStore"
	TypeObservability = "Observability"
	TypeK8sResource   = "K8sResource"
)

func KnownTypes() []string {
	return []string{
		TypeCompute,
		TypeImageBuild,
		TypeCDKStack,
		TypeDatabase,
		TypeSQLDatabase,
		TypeVolume,
		TypeConfig,
		TypeNetwork,
		TypeSecret,
		TypeLoadBalancer,
		TypeTraefikRoute,
		TypeObjectStore,
		TypeObservability,
		TypeK8sResource,
	}
}
