package awsdriver

import (
	"context"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
)

type BackendAPI interface {
	// CloudFormation / CDK
	Synthesize(ctx context.Context, req StackRequest) error
	CheckEnvironment(ctx context.Context, req StackRequest) error
	GetStack(ctx context.Context, req StackRequest) (StackState, bool, error)
	DetectDrift(ctx context.Context, req StackRequest) (StackDriftStatus, error)
	DeployStack(ctx context.Context, req StackRequest) (StackState, error)
	DeleteStack(ctx context.Context, req StackRequest) error

	// EFS
	UpsertFileSystem(ctx context.Context, fs FileSystem) (string, error)
	GetFileSystem(ctx context.Context, fsID string) (FileSystem, bool, error)
	DeleteFileSystem(ctx context.Context, fsID string) error

	// VPC
	UpsertVPC(ctx context.Context, vpc VPC) (string, error)
	GetVPC(ctx context.Context, vpcID string) (VPC, bool, error)
	DeleteVPC(ctx context.Context, vpcID string) error

	// RDS (Aurora)
	UpsertDBCluster(ctx context.Context, cluster DBCluster) (DBCluster, error)
	GetDBCluster(ctx context.Context, name string) (DBCluster, bool, error)
	DeleteDBCluster(ctx context.Context, arn, name string) error

	// SSM Parameter Store
	UpsertParameter(ctx context.Context, param Parameter) error
	GetParameter(ctx context.Context, name string) (Parameter, bool, error)
	DeleteParameter(ctx context.Context, name string) error

	// Secrets Manager
	UpsertSecret(ctx context.Context, secret Secret) (string, error)
	GetSecret(ctx context.Context, secretID string) (Secret, bool, error)
	DeleteSecret(ctx context.Context, secretID string) error

	// ECS
	UpsertService(ctx context.Context, svc ECSService) error
	GetService(ctx context.Context, cluster, name string) (ECSService, bool, error)
	DeleteService(ctx context.Context, cluster, name string) error
	AttachServiceToTargetGroup(ctx context.Context, cluster, service, container, tgARN string, port int, sgID string) error
	EnsureObservabilityTaskRole(ctx context.Context) (string, error)

	// ELBv2 (ALB/NLB)
	UpsertLoadBalancer(ctx context.Context, lb LoadBalancer) (string, error)
	GetLoadBalancer(ctx context.Context, name string) (LoadBalancer, bool, error)
	DeleteLoadBalancer(ctx context.Context, arn string) error
	EnsureLoadBalancerSecurityGroup(ctx context.Context, name, scheme string, ports []int) (string, error)

	UpsertTargetGroup(ctx context.Context, tg TargetGroup) (string, error)
	GetTargetGroup(ctx context.Context, name string) (TargetGroup, bool, error)
	DeleteTargetGroup(ctx context.Context, arn string) error

	UpsertListener(ctx context.Context, listener Listener) (string, error)
	GetListener(ctx context.Context, lbARN string, port int) (Listener, bool, error)
	DeleteListener(ctx context.Context, arn string) error

	// S3
	UpsertBucket(ctx context.Context, bucket BucketSpec) error
	GetBucket(ctx context.Context, name string) (BucketSpec, bool, error)
	DeleteBucket(ctx context.Context, name string) error

	// CloudWatch Logs
	UpsertLogGroup(ctx context.Context, group LogGroup) error
	GetLogGroup(ctx context.Context, name string) (LogGroup, bool, error)
}

// --- Resource types ---

type FileSystem struct {
	ID           string
	Name         string
	Hash         string
	Tags         map[string]string
	Encrypted    bool
	AccessPoints map[string]string
}

// VPC is a managed EC2 network boundary.
type VPC struct {
	ID        string
	Name      string
	Hash      string
	Tags      map[string]string
	CIDRBlock string
	Internal  bool
}

// DBCluster is a managed RDS Aurora cluster.
type DBCluster struct {
	ARN              string
	Name             string
	Hash             string
	Tags             map[string]string
	Engine           string
	EngineVersion    string
	DatabaseName     string
	MasterUsername   string
	Port             int
	Endpoint         string
	ReaderEndpoint   string
	MasterUserSecret string
	Status           string
	SubnetGroup      string
	SecurityGroups   []string
}

type Parameter struct {
	Name  string
	Type  string
	Value string
	Hash  string
	Tags  map[string]string
}

type Secret struct {
	ID    string
	Name  string
	Value string
	Hash  string
	Tags  map[string]string
}

type ECSService struct {
	Name           string
	Cluster        string
	Hash           string
	Tags           map[string]string
	DesiredCount   int
	LaunchType     string
	TaskFamily     string
	TaskRole       string
	ExecRole       string
	Subnets        []string
	SecurityGroups []string
	AssignPublicIP bool
	Container      ContainerDef
	Sidecars       []ContainerDef
	TargetGroupARN string
	LogGroup       string
	ServiceConnect *ServiceConnectConfig
}

// ServiceConnectConfig registers an ECS service in the cluster's Service
// Connect namespace so it gets a stable DNS name.
type ServiceConnectConfig struct {
	Namespace string
	Services  []ServiceConnectService
}

type ServiceConnectService struct {
	PortMappingName string
	DNSName         string
	Port            int
}

type ContainerDef struct {
	Name        string
	Image       string
	Command     []string
	Args        []string
	Env         map[string]string
	Secrets     map[string]string
	Ports       []PortDef
	CPU         int
	Memory      int
	GPUs        int
	HealthCheck *HealthCheckDef
	Privileged  bool
	Essential   *bool
	Volumes     []VolumeDef
	LogDriver   string
	LogOptions  map[string]string
}

type PortDef struct {
	Name          string
	ContainerPort int
	HostPort      int
	Protocol      string
}

type HealthCheckDef struct {
	Command     []string
	Interval    int
	Timeout     int
	Retries     int
	StartPeriod int
}

type VolumeDef struct {
	Name            string
	EFSFileSystemID string
	AccessPointID   string
	RootPath        string
}

type LoadBalancer struct {
	ARN            string
	Name           string
	Hash           string
	Tags           map[string]string
	Type           string
	Scheme         string
	Subnets        []string
	SecurityGroups []string
	DNSName        string
}

type TargetGroup struct {
	ARN         string
	Name        string
	Hash        string
	Tags        map[string]string
	Port        int
	Protocol    string
	TargetType  string
	VPCID       string
	HealthPath  string
	HealthProto string
	HealthPort  string
}

type Listener struct {
	ARN              string
	Hash             string
	Tags             map[string]string
	LoadBalancerARN  string
	Port             int
	Protocol         string
	DefaultActionARN string
}

type BucketSpec struct {
	Name       string
	Hash       string
	Tags       map[string]string
	Versioning bool
	Region     string
}

type LogGroup struct {
	Name string
	Hash string
	Tags map[string]string
}

// --- CDK types (unchanged) ---

// ExecutionRoleManager is implemented by backends that can provision
// per-intent CloudFormation execution roles (the AWS SDK backend). Backends
// without it fall back to the account-wide bootstrap role.
type ExecutionRoleManager interface {
	EnsureExecutionRole(ctx context.Context, roleName string, actions []string) (string, error)
	DeleteExecutionRole(ctx context.Context, roleName string) error
}

type StackRequest struct {
	PartitionName string
	IntentName    string
	AssetName     string
	Target        targetdomain.Placement
	Manifest      stackPayload
	WorkspaceDir  string
	AppCommand    string
	Context       map[string]string
	Env           map[string]string
	DesiredHash   string
	Tags          map[string]string
	// ExecutionRoleARN is passed to `cdk deploy --role-arn` when a per-intent
	// execution role is provisioned for the stack.
	ExecutionRoleARN string
}

type StackState struct {
	ID      string
	Name    string
	Status  string
	Tags    map[string]string
	Outputs map[string]string
}

type StackDriftStatus string

const (
	StackDriftUnknown StackDriftStatus = "UNKNOWN"
	StackDriftInSync  StackDriftStatus = "IN_SYNC"
	StackDriftDrifted StackDriftStatus = "DRIFTED"
)
