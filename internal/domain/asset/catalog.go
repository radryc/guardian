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
	TypeProbe         = "Probe"
	TypeK8sResource   = "K8sResource"
	// TypeAWSResource is an internal asset type for AWS resources that have no
	// curated Guardian template yet. It is rendered by the fallback
	// `aws.fallback.cfn-resource` template with a raw CloudFormation type.
	TypeAWSResource = "AWSResource"
	// TypeObservedResource is an internal, read-only asset type for live
	// resources discovered from a non-AWS provider inventory (GCP Cloud Asset
	// Inventory, Kubernetes). It is never authored in an intent manifest.
	TypeObservedResource = "ObservedResource"
	// TypeGeneratedStack is an internal, non-user-authorable asset type that
	// carries the whole intent's generated CDK stack through the task pipeline.
	TypeGeneratedStack = "GeneratedStack"
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
		TypeProbe,
		TypeK8sResource,
	}
}
