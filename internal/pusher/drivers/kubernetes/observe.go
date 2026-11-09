package kubernetesdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/radryc/guardian/internal/k8sobserve"
)

type managedResourceType struct {
	gvr        schema.GroupVersionResource
	kind       string
	apiVersion string
}

var managedResourceTypes = []managedResourceType{
	{gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, kind: "Deployment", apiVersion: "apps/v1"},
	{gvr: servicesGVR, kind: "Service", apiVersion: "v1"},
	{gvr: configMapsGVR, kind: "ConfigMap", apiVersion: "v1"},
	{gvr: claimsGVR, kind: "PersistentVolumeClaim", apiVersion: "v1"},
	{gvr: networkPoliciesGVR, kind: "NetworkPolicy", apiVersion: "networking.k8s.io/v1"},
	{gvr: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, kind: "Secret", apiVersion: "v1"},
	{gvr: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, kind: "ServiceAccount", apiVersion: "v1"},
	{gvr: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, kind: "Ingress", apiVersion: "networking.k8s.io/v1"},
}

// ListManagedResources returns every Guardian-managed object in the cluster.
// Secret and ConfigMap data are intentionally omitted from the inventory.
func (c *ClientBackend) ListManagedResources(ctx context.Context) ([]k8sobserve.Resource, error) {
	type listResult struct {
		resources []k8sobserve.Resource
	}
	results := make([]listResult, len(managedResourceTypes))
	var wg sync.WaitGroup
	for index := range managedResourceTypes {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			resourceType := managedResourceTypes[index]
			list, err := c.client.Resource(resourceType.gvr).List(ctx, metav1.ListOptions{
				LabelSelector: "guardian.managed=true",
			})
			if err != nil {
				// A missing API group (for example NetworkPolicy on a very small
				// cluster) must not hide every other resource.
				return
			}
			resources := make([]k8sobserve.Resource, 0, len(list.Items))
			for i := range list.Items {
				resource, ok := managedResourceFromObject(resourceType.kind, resourceType.apiVersion, list.Items[i].Object)
				if ok {
					resources = append(resources, resource)
				}
			}
			results[index].resources = resources
		}(index)
	}
	wg.Wait()
	out := make([]k8sobserve.Resource, 0)
	for _, result := range results {
		out = append(out, result.resources...)
	}
	return out, nil
}

// ListManagedResources implements the same interface with kubectl.
func (b *CLIBackend) ListManagedResources(ctx context.Context) ([]k8sobserve.Resource, error) {
	kinds := "deployments,services,configmaps,persistentvolumeclaims,networkpolicies,secrets,serviceaccounts,ingresses"
	args := append(b.baseArgs(), "get", kinds, "-A", "-l", "guardian.managed=true", "-o", "json")
	raw, err := b.run(args...)
	if err != nil {
		return nil, err
	}
	var response struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode kubectl get output: %w", err)
	}
	out := make([]k8sobserve.Resource, 0, len(response.Items))
	for _, item := range response.Items {
		kind, _ := item["kind"].(string)
		apiVersion, _ := item["apiVersion"].(string)
		resource, ok := managedResourceFromObject(kind, apiVersion, item)
		if ok {
			out = append(out, resource)
		}
	}
	return out, nil
}

func managedResourceFromObject(kind, apiVersion string, object map[string]any) (k8sobserve.Resource, bool) {
	metadata, _ := object["metadata"].(map[string]any)
	if metadata == nil {
		return k8sobserve.Resource{}, false
	}
	name := stringValue(metadata["name"])
	namespace := stringValue(metadata["namespace"])
	if name == "" {
		return k8sobserve.Resource{}, false
	}
	labels := stringMap(metadata["labels"])
	partition := strings.TrimSpace(labels["guardian.partition"])
	intent := strings.TrimSpace(labels["guardian.intent"])
	if partition == "" || intent == "" {
		return k8sobserve.Resource{}, false
	}
	resource := k8sobserve.Resource{
		Partition:    partition,
		Intent:       intent,
		Asset:        strings.TrimSpace(labels["guardian.asset"]),
		AssetType:    strings.TrimSpace(labels["guardian.asset.type"]),
		Cluster:      strings.TrimSpace(labels["guardian.cluster"]),
		Namespace:    namespace,
		Kind:         kind,
		APIVersion:   apiVersion,
		Name:         name,
		UID:          stringValue(metadata["uid"]),
		Labels:       labels,
		ManagedAsset: strings.TrimSpace(labels["guardian.asset"]),
		ManagedBy:    strings.TrimSpace(labels["guardian.managed"]),
		CreatedAt:    parseTime(metadata["creationTimestamp"]),
	}
	if owners, ok := metadata["ownerReferences"].([]any); ok && len(owners) > 0 {
		if owner, ok := owners[0].(map[string]any); ok {
			resource.OwnerReference = strings.TrimSpace(stringValue(owner["kind"]) + "/" + stringValue(owner["name"]))
		}
	}
	status, _ := object["status"].(map[string]any)
	spec, _ := object["spec"].(map[string]any)
	data, _ := object["data"].(map[string]any)
	applyResourceStatus(&resource, status, spec, data)
	return resource, true
}

func applyResourceStatus(resource *k8sobserve.Resource, status, spec, data map[string]any) {
	switch resource.Kind {
	case "Deployment":
		resource.Replicas = intValue(status["replicas"])
		resource.Available = intValue(status["availableReplicas"])
		if resource.Replicas == 0 {
			resource.Replicas = intValue(spec["replicas"])
		}
		resource.Ready = fmt.Sprintf("%d/%d ready", resource.Available, resource.Replicas)
		if resource.Replicas <= 0 || resource.Available >= resource.Replicas {
			resource.Status = "Available"
		} else {
			resource.Status = "Progressing"
		}
	case "Service":
		serviceType := stringValue(spec["type"])
		if serviceType == "" {
			serviceType = "ClusterIP"
		}
		resource.Status = serviceType
		resource.Ready = strings.TrimSpace(stringValue(spec["clusterIP"]))
	case "PersistentVolumeClaim":
		resource.Status = stringValue(status["phase"])
		if capacity, ok := status["capacity"].(map[string]any); ok {
			resource.Ready = stringValue(capacity["storage"])
		}
	case "ConfigMap", "Secret":
		resource.Status = "Present"
		resource.Ready = fmt.Sprintf("%d keys", len(data))
	case "ServiceAccount":
		resource.Status = "Present"
	case "NetworkPolicy":
		resource.Status = "Applied"
	case "Ingress":
		resource.Status = "Pending"
		if loadBalancer, ok := status["loadBalancer"].(map[string]any); ok {
			if ingress, ok := loadBalancer["ingress"].([]any); ok && len(ingress) > 0 {
				resource.Status = "Ready"
				if first, ok := ingress[0].(map[string]any); ok {
					resource.Ready = strings.TrimSpace(stringValue(first["hostname"]) + stringValue(first["ip"]))
				}
			}
		}
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func stringMap(value any) map[string]string {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, item := range raw {
		if text, ok := item.(string); ok {
			out[key] = text
		}
	}
	return out
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func parseTime(value any) time.Time {
	text, ok := value.(string)
	if !ok || text == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, text)
	return parsed
}
