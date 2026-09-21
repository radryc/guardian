package awsscan

import (
	"encoding/json"
	"time"
)

const (
	APIVersion  = "guardian/v1alpha1"
	RequestKind = "AWSScanRequest"
	ResultKind  = "AWSScanResult"

	InventoryDetailSummary = "summary"
	InventoryDetailFull    = "full"

	AllRegions = "all"
)

type ScanRequest struct {
	APIVersion           string    `json:"apiVersion"`
	Kind                 string    `json:"kind"`
	ScanID               string    `json:"scanID"`
	Account              string    `json:"account"`
	Regions              []string  `json:"regions"`
	IncludeResourceTypes []string  `json:"includeResourceTypes,omitempty"`
	ExcludeResourceTypes []string  `json:"excludeResourceTypes,omitempty"`
	Inventory            bool      `json:"inventory,omitempty"`
	InventoryDetail      string    `json:"inventoryDetail,omitempty"`
	CreatedAt            time.Time `json:"createdAt"`
	RequestedBy          string    `json:"requestedBy,omitempty"`
}

type ScanStatus string

const (
	ScanStatusQueued    ScanStatus = "Queued"
	ScanStatusRunning   ScanStatus = "Running"
	ScanStatusSucceeded ScanStatus = "Succeeded"
	ScanStatusFailed    ScanStatus = "Failed"
)

type ScanClaim struct {
	ScanID       string    `json:"scanID"`
	WorkerID     string    `json:"workerID"`
	ClaimedAt    time.Time `json:"claimedAt"`
	LeaseSeconds int       `json:"leaseSeconds"`
}

type ManagedInfo struct {
	Managed             bool   `json:"managed"`
	Partition           string `json:"partition,omitempty"`
	Intent              string `json:"intent,omitempty"`
	Asset               string `json:"asset,omitempty"`
	AssetType           string `json:"assetType,omitempty"`
	Hash                string `json:"hash,omitempty"`
}

type BucketResource struct {
	Name      string            `json:"name"`
	Region    string            `json:"region"`
	Versioned bool              `json:"versioned"`
	Tags      map[string]string `json:"tags,omitempty"`
	Managed   ManagedInfo       `json:"managed"`
	Stack     string            `json:"stack,omitempty"`
}

type FileSystemResource struct {
	ID             string            `json:"id"`
	CreationToken  string            `json:"creationToken,omitempty"`
	Name           string            `json:"name,omitempty"`
	Region         string            `json:"region"`
	LifeCycleState string            `json:"lifeCycleState,omitempty"`
	Encrypted      bool              `json:"encrypted"`
	Tags           map[string]string `json:"tags,omitempty"`
	Managed        ManagedInfo       `json:"managed"`
	Stack          string            `json:"stack,omitempty"`
}

type ParameterResource struct {
	Name    string            `json:"name"`
	Region  string            `json:"region"`
	Type    string            `json:"type,omitempty"`
	Value   string            `json:"value,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
	Managed ManagedInfo       `json:"managed"`
	Stack   string            `json:"stack,omitempty"`
}

type SecretResource struct {
	ARN         string            `json:"arn"`
	Name        string            `json:"name"`
	Region      string            `json:"region"`
	Description string            `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Managed     ManagedInfo       `json:"managed"`
	Stack       string            `json:"stack,omitempty"`
}

type ServicePort struct {
	Name          string `json:"name,omitempty"`
	ContainerPort int    `json:"containerPort"`
	HostPort      int    `json:"hostPort,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
}

type ServiceResource struct {
	Region         string            `json:"region"`
	Cluster        string            `json:"cluster"`
	ClusterName    string            `json:"clusterName"`
	Name           string            `json:"name"`
	Status         string            `json:"status,omitempty"`
	DesiredCount   int               `json:"desiredCount"`
	LaunchType     string            `json:"launchType,omitempty"`
	TaskFamily     string            `json:"taskFamily,omitempty"`
	TaskRole       string            `json:"taskRole,omitempty"`
	ExecRole       string            `json:"execRole,omitempty"`
	Image          string            `json:"image,omitempty"`
	CPU            int               `json:"cpu,omitempty"`
	Memory         int               `json:"memory,omitempty"`
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Ports          []ServicePort     `json:"ports,omitempty"`
	Subnets        []string          `json:"subnets,omitempty"`
	SecurityGroups []string          `json:"securityGroups,omitempty"`
	AssignPublicIP bool              `json:"assignPublicIP"`
	LogGroup       string            `json:"logGroup,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
	Managed        ManagedInfo       `json:"managed"`
	Stack          string            `json:"stack,omitempty"`
}

type ListenerResource struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol,omitempty"`
}

type TargetGroupResource struct {
	Name       string `json:"name"`
	Port       int    `json:"port,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	TargetType string `json:"targetType,omitempty"`
}

type LoadBalancerResource struct {
	Region         string                `json:"region"`
	ARN            string                `json:"arn"`
	Name           string                `json:"name"`
	Type           string                `json:"type,omitempty"`
	Scheme         string                `json:"scheme,omitempty"`
	DNSName        string                `json:"dnsName,omitempty"`
	VPCID          string                `json:"vpcId,omitempty"`
	Subnets        []string              `json:"subnets,omitempty"`
	SecurityGroups []string              `json:"securityGroups,omitempty"`
	Listeners      []ListenerResource    `json:"listeners,omitempty"`
	TargetGroups   []TargetGroupResource `json:"targetGroups,omitempty"`
	Tags           map[string]string     `json:"tags,omitempty"`
	Managed        ManagedInfo           `json:"managed"`
	Stack          string                `json:"stack,omitempty"`
}

type StackResource struct {
	Region      string            `json:"region"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Status      string            `json:"status,omitempty"`
	DriftStatus string            `json:"driftStatus,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	ResourceIDs map[string]string `json:"resourceIds,omitempty"`
	Managed     ManagedInfo       `json:"managed"`
}

type InventoryResource struct {
	Identifier string            `json:"identifier"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties json.RawMessage   `json:"properties,omitempty"`
}

type ScanError struct {
	Region       string `json:"region,omitempty"`
	ResourceType string `json:"resourceType,omitempty"`
	Message      string `json:"message"`
}

type ScanSummary struct {
	RegionCount       int `json:"regionCount"`
	BucketCount       int `json:"bucketCount"`
	FileSystemCount   int `json:"fileSystemCount"`
	ParameterCount    int `json:"parameterCount"`
	SecretCount       int `json:"secretCount"`
	ServiceCount      int `json:"serviceCount"`
	LoadBalancerCount int `json:"loadBalancerCount"`
	StackCount        int `json:"stackCount"`
	InventoryCount    int `json:"inventoryCount"`
	ManagedCount      int `json:"managedCount"`
	ErrorCount        int `json:"errorCount"`
}

type ScanResult struct {
	APIVersion    string                                   `json:"apiVersion"`
	Kind          string                                   `json:"kind"`
	ScanID        string                                   `json:"scanID"`
	Pusher        string                                   `json:"pusher"`
	Account       string                                   `json:"account"`
	Status        ScanStatus                               `json:"status"`
	StartedAt     time.Time                                `json:"startedAt"`
	FinishedAt    time.Time                                `json:"finishedAt"`
	Request       *ScanRequest                             `json:"request,omitempty"`
	Regions       []string                                 `json:"regions,omitempty"`
	Buckets       []BucketResource                         `json:"buckets,omitempty"`
	FileSystems   []FileSystemResource                     `json:"fileSystems,omitempty"`
	Parameters    []ParameterResource                      `json:"parameters,omitempty"`
	Secrets       []SecretResource                         `json:"secrets,omitempty"`
	Services      []ServiceResource                        `json:"services,omitempty"`
	LoadBalancers []LoadBalancerResource                   `json:"loadBalancers,omitempty"`
	Stacks        []StackResource                          `json:"stacks,omitempty"`
	Inventory     map[string]map[string][]InventoryResource `json:"inventory,omitempty"`
	Errors        []ScanError                              `json:"errors,omitempty"`
	Summary       ScanSummary                              `json:"summary"`
}

func (r *ScanResult) CountManaged() int {
	count := 0
	for _, b := range r.Buckets {
		if b.Managed.Managed {
			count++
		}
	}
	for _, fs := range r.FileSystems {
		if fs.Managed.Managed {
			count++
		}
	}
	for _, p := range r.Parameters {
		if p.Managed.Managed {
			count++
		}
	}
	for _, s := range r.Secrets {
		if s.Managed.Managed {
			count++
		}
	}
	for _, s := range r.Services {
		if s.Managed.Managed {
			count++
		}
	}
	for _, lb := range r.LoadBalancers {
		if lb.Managed.Managed {
			count++
		}
	}
	for _, st := range r.Stacks {
		if st.Managed.Managed {
			count++
		}
	}
	return count
}

func (r *ScanResult) CountInventory() int {
	count := 0
	for _, byType := range r.Inventory {
		for _, resources := range byType {
			count += len(resources)
		}
	}
	return count
}
