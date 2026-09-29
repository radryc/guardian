package kubernetesdriver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// ClientBackend implements BackendAPI against the Kubernetes API directly using
// client-go, avoiding a kubectl subprocess per operation. Server-side apply
// provides idempotent create-or-update with a stable field manager.
type ClientBackend struct {
	client       dynamic.Interface
	mapper       *restmapper.DeferredDiscoveryRESTMapper
	fieldManager string
}

var _ BackendAPI = (*ClientBackend)(nil)

var (
	configMapsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	claimsGVR     = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}
	servicesGVR   = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
	namespacesGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	podsGVR       = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	eventsGVR     = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "events"}
)

// NewClientBackend builds a client-go backend. kubeconfig/contextName follow the
// same semantics as the kubectl backend: empty kubeconfig tries in-cluster
// credentials first, then the default loading rules.
func NewClientBackend(kubeconfig, contextName string) (*ClientBackend, error) {
	cfg, err := loadRESTConfig(kubeconfig, contextName)
	if err != nil {
		return nil, err
	}
	cfg.QPS = 50
	cfg.Burst = 100
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %w", err)
	}
	return &ClientBackend{
		client:       client,
		mapper:       restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient)),
		fieldManager: "guardian",
	}, nil
}

func loadRESTConfig(kubeconfig, contextName string) (*rest.Config, error) {
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	if strings.TrimSpace(kubeconfig) != "" {
		return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
			overrides,
		).ClientConfig()
	}
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		overrides,
	).ClientConfig()
}

func (c *ClientBackend) resourceFor(gvk schema.GroupVersionKind) (dynamic.NamespaceableResourceInterface, error) {
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("resolve resource for %s: %w", gvk, err)
	}
	return c.client.Resource(mapping.Resource), nil
}

func (c *ClientBackend) applyObject(ctx context.Context, obj map[string]any) error {
	u := &unstructured.Unstructured{Object: obj}
	if u.GetName() == "" {
		return fmt.Errorf("manifest for %s is missing metadata.name", u.GroupVersionKind())
	}
	resource, err := c.resourceFor(u.GroupVersionKind())
	if err != nil {
		return err
	}
	var iface dynamic.ResourceInterface = resource
	if ns := u.GetNamespace(); ns != "" {
		iface = resource.Namespace(ns)
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("encode manifest %s: %w", u.GetName(), err)
	}
	force := true
	_, err = iface.Patch(ctx, u.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: c.fieldManager,
		Force:        &force,
	})
	if err != nil {
		return fmt.Errorf("apply %s/%s: %w", u.GetKind(), u.GetName(), err)
	}
	return nil
}

func (c *ClientBackend) getObject(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) ([]byte, bool, error) {
	var iface dynamic.ResourceInterface = c.client.Resource(gvr)
	if namespace != "" {
		iface = c.client.Resource(gvr).Namespace(namespace)
	}
	u, err := iface.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	data, err := json.Marshal(u.Object)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (c *ClientBackend) deleteObject(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) error {
	var iface dynamic.ResourceInterface = c.client.Resource(gvr)
	if namespace != "" {
		iface = c.client.Resource(gvr).Namespace(namespace)
	}
	err := iface.Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (c *ClientBackend) ensureNamespace(ctx context.Context, namespace string) error {
	if namespace == "" || namespace == "default" {
		return nil
	}
	if _, err := c.client.Resource(namespacesGVR).Get(ctx, namespace, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	ns := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": namespace},
	}}
	if _, err := c.client.Resource(namespacesGVR).Create(ctx, ns, metav1.CreateOptions{FieldManager: c.fieldManager}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (c *ClientBackend) UpsertConfigMap(cm ConfigMap) error {
	ctx := context.Background()
	if err := c.ensureNamespace(ctx, cm.Namespace); err != nil {
		return err
	}
	return c.applyObject(ctx, configMapManifest(cm))
}

func (c *ClientBackend) GetConfigMap(namespace, name string) (ConfigMap, bool, error) {
	raw, ok, err := c.getObject(context.Background(), configMapsGVR, namespace, name)
	if err != nil || !ok {
		return ConfigMap{}, ok, err
	}
	cm, err := decodeConfigMap(namespace, raw)
	if err != nil {
		return ConfigMap{}, false, err
	}
	return cm, true, nil
}

func (c *ClientBackend) DeleteConfigMap(namespace, name string) error {
	return c.deleteObject(context.Background(), configMapsGVR, namespace, name)
}

func (c *ClientBackend) UpsertClaim(claim PersistentVolumeClaim) error {
	ctx := context.Background()
	if err := c.ensureNamespace(ctx, claim.Namespace); err != nil {
		return err
	}
	return c.applyObject(ctx, claimManifest(claim))
}

func (c *ClientBackend) GetClaim(namespace, name string) (PersistentVolumeClaim, bool, error) {
	raw, ok, err := c.getObject(context.Background(), claimsGVR, namespace, name)
	if err != nil || !ok {
		return PersistentVolumeClaim{}, ok, err
	}
	claim, err := decodeClaim(namespace, raw)
	if err != nil {
		return PersistentVolumeClaim{}, false, err
	}
	return claim, true, nil
}

func (c *ClientBackend) DeleteClaim(namespace, name string) error {
	return c.deleteObject(context.Background(), claimsGVR, namespace, name)
}

func (c *ClientBackend) UpsertDeployment(deployment Deployment) error {
	ctx := context.Background()
	if err := c.ensureNamespace(ctx, deployment.Namespace); err != nil {
		return err
	}
	for _, item := range deploymentManifestItems(deployment) {
		if err := c.applyObject(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (c *ClientBackend) GetDeployment(namespace, name string) (Deployment, bool, error) {
	ctx := context.Background()
	deploymentGVR, err := c.deploymentGVR()
	if err != nil {
		return Deployment{}, false, err
	}
	raw, ok, err := c.getObject(ctx, deploymentGVR, namespace, name)
	if err != nil || !ok {
		return Deployment{}, ok, err
	}
	deployment, err := decodeDeployment(namespace, raw)
	if err != nil {
		return Deployment{}, false, err
	}
	if deployment.ReadyReplicas < deployment.Replicas {
		reason, message, podName := c.podsTerminalFailure(ctx, namespace, deployment.Labels)
		deployment.CrashLoopBackOff = reason != ""
		deployment.PodFailureReason = reason
		deployment.PodFailureMessage = message
		deployment.PodFailurePodName = podName
	}
	return deployment, true, nil
}

func (c *ClientBackend) deploymentGVR() (schema.GroupVersionResource, error) {
	mapping, err := c.mapper.RESTMapping(schema.GroupKind{Group: "apps", Kind: "Deployment"}, "v1")
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("resolve deployment resource: %w", err)
	}
	return mapping.Resource, nil
}

func (c *ClientBackend) DeleteDeployment(namespace, name string) error {
	ctx := context.Background()
	deploymentGVR, err := c.deploymentGVR()
	if err != nil {
		return err
	}
	if err := c.deleteObject(ctx, deploymentGVR, namespace, name); err != nil {
		return err
	}
	return c.deleteObject(ctx, configMapsGVR, namespace, inlineConfigMapName(name))
}

func (c *ClientBackend) UpsertService(service Service) error {
	ctx := context.Background()
	if err := c.ensureNamespace(ctx, service.Namespace); err != nil {
		return err
	}
	return c.applyObject(ctx, serviceManifest(service))
}

func (c *ClientBackend) GetService(namespace, name string) (Service, bool, error) {
	raw, ok, err := c.getObject(context.Background(), servicesGVR, namespace, name)
	if err != nil || !ok {
		return Service{}, ok, err
	}
	service, err := decodeService(namespace, raw)
	if err != nil {
		return Service{}, false, err
	}
	return service, true, nil
}

func (c *ClientBackend) DeleteService(namespace, name string) error {
	return c.deleteObject(context.Background(), servicesGVR, namespace, name)
}

func (c *ClientBackend) podsTerminalFailure(ctx context.Context, namespace string, labels map[string]string) (reason, message, podName string) {
	selector := selectorForLabels(labels)
	if len(selector) == 0 {
		return "", "", ""
	}
	parts := make([]string, 0, len(selector))
	for k, v := range selector {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	list, err := c.client.Resource(podsGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: strings.Join(parts, ","),
	})
	if err != nil {
		return "", "", ""
	}
	for _, pod := range list.Items {
		name := pod.GetName()
		if statuses, found, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses"); found {
			if r, m, ok := waitingFailure(statuses); ok {
				return r, m, name
			}
		}
		if statuses, found, _ := unstructured.NestedSlice(pod.Object, "status", "initContainerStatuses"); found {
			if r, m, ok := waitingFailure(statuses); ok {
				return r, m, name
			}
		}
	}
	return "", "", ""
}

func waitingFailure(statuses []any) (reason, message string, found bool) {
	for _, status := range statuses {
		entry, ok := status.(map[string]any)
		if !ok {
			continue
		}
		waitingReason, _, _ := unstructured.NestedString(entry, "state", "waiting", "reason")
		if waitingReason == "" || transientPodWaitingReasons[waitingReason] {
			continue
		}
		waitingMessage, _, _ := unstructured.NestedString(entry, "state", "waiting", "message")
		return waitingReason, waitingMessage, true
	}
	return "", "", false
}

func (c *ClientBackend) GetPodEvents(namespace, podName string) ([]string, error) {
	if podName == "" {
		return nil, nil
	}
	list, err := c.client.Resource(eventsGVR).Namespace(namespace).List(context.Background(), metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + podName,
	})
	if err != nil {
		return nil, nil
	}
	entries := make([]string, 0, len(list.Items))
	for _, event := range list.Items {
		message, _, _ := unstructured.NestedString(event.Object, "message")
		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}
		reason, _, _ := unstructured.NestedString(event.Object, "reason")
		eventType, _, _ := unstructured.NestedString(event.Object, "type")
		line := reason
		if eventType != "" {
			line = eventType + " " + reason
		}
		if count, ok, _ := unstructured.NestedInt64(event.Object, "count"); ok && count > 1 {
			line += fmt.Sprintf(" (x%d)", count)
		}
		line += ": " + message
		if ts, ok, _ := unstructured.NestedString(event.Object, "lastTimestamp"); ok && ts != "" {
			line += " (" + ts + ")"
		}
		entries = append(entries, line)
	}
	return entries, nil
}

// ApplyManifest applies a raw (possibly multi-document) manifest via
// server-side apply. When namespace is non-empty it is applied to documents
// that do not carry their own namespace.
func (c *ClientBackend) ApplyManifest(namespace string, manifest []byte) error {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(manifest), 4096)
	for {
		var obj map[string]any
		if err := decoder.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("decode manifest: %w", err)
		}
		if len(obj) == 0 {
			continue
		}
		if namespace != "" {
			metadata, _ := obj["metadata"].(map[string]any)
			if metadata == nil {
				metadata = map[string]any{}
				obj["metadata"] = metadata
			}
			if existing, _ := metadata["namespace"].(string); strings.TrimSpace(existing) == "" {
				metadata["namespace"] = namespace
			}
		}
		if err := c.applyObject(context.Background(), obj); err != nil {
			return err
		}
	}
	return nil
}
