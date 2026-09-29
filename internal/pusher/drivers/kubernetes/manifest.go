package kubernetesdriver

import (
	"fmt"
	"strings"
)

// This file contains the pure manifest builders shared by the kubectl-based
// CLIBackend and the client-go ClientBackend. They translate Guardian's
// internal Deployment/Service/ConfigMap/PVC types into unstructured Kubernetes
// objects so both backends produce identical desired state.

func configMapManifest(cm ConfigMap) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      cm.Name,
			"namespace": cm.Namespace,
			"labels":    cloneStringMap(cm.Labels),
			"annotations": map[string]string{
				fullHashAnnotation: cm.Hash,
			},
		},
		"data": cloneStringMap(cm.Data),
	}
}

func claimManifest(claim PersistentVolumeClaim) map[string]any {
	spec := map[string]any{
		"accessModes": []string{firstNonEmpty(claim.AccessMode, "ReadWriteOnce")},
		"resources": map[string]any{
			"requests": map[string]string{
				"storage": firstNonEmpty(claim.Size, "1Gi"),
			},
		},
	}
	if claim.StorageClass != "" {
		spec["storageClassName"] = claim.StorageClass
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata": map[string]any{
			"name":      claim.Name,
			"namespace": claim.Namespace,
			"labels":    cloneStringMap(claim.Labels),
			"annotations": map[string]string{
				fullHashAnnotation: claim.Hash,
			},
		},
		"spec": spec,
	}
}

func serviceManifest(service Service) map[string]any {
	annotations := cloneStringMap(service.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[fullHashAnnotation] = service.Hash
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":        service.Name,
			"namespace":   service.Namespace,
			"labels":      cloneStringMap(service.Labels),
			"annotations": annotations,
		},
		"spec": map[string]any{
			"type":     firstNonEmpty(service.Type, "ClusterIP"),
			"selector": cloneStringMap(service.Selector),
			"ports":    servicePorts(service.Ports),
		},
	}
}

func deploymentManifestItems(deployment Deployment) []map[string]any {
	items := []map[string]any{}
	volumeItems, volumeMounts := containerVolumes(deployment)
	if len(deployment.Container.InlineFiles) > 0 {
		items = append(items, map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      inlineConfigMapName(deployment.Name),
				"namespace": deployment.Namespace,
				"labels":    cloneStringMap(deployment.Labels),
			},
			"data": cloneStringMap(deployment.Container.InlineFiles),
		})
	}
	selector := selectorForLabels(deployment.Labels)
	replicas := deployment.Replicas
	if replicas < 1 {
		replicas = 1
	}
	containerSpec := map[string]any{
		"name":            firstNonEmpty(deployment.Container.Name, deployment.Name),
		"image":           deployment.Container.Image,
		"env":             envList(deployment.Container.Env),
		"ports":           containerPorts(deployment.Container.Ports),
		"volumeMounts":    volumeMounts,
		"securityContext": containerSecurityContext(deployment.Container),
	}
	if len(deployment.Container.Command) > 0 {
		containerSpec["command"] = append([]string(nil), deployment.Container.Command...)
	}
	if len(deployment.Container.Args) > 0 {
		containerSpec["args"] = append([]string(nil), deployment.Container.Args...)
	}
	if deployment.Container.ImagePullPolicy != "" {
		containerSpec["imagePullPolicy"] = deployment.Container.ImagePullPolicy
	}
	if probe := deployment.Container.ReadinessProbe; probe != nil {
		containerSpec["readinessProbe"] = probeSpec(probe)
	}
	if r := deployment.Container.Resources; r.CPURequest != "" || r.CPULimit != "" || r.MemoryRequest != "" || r.MemoryLimit != "" || len(r.ExtendedResources) > 0 {
		resources := map[string]any{}
		if req := map[string]string{}; r.CPURequest != "" || r.MemoryRequest != "" {
			if r.CPURequest != "" {
				req["cpu"] = r.CPURequest
			}
			if r.MemoryRequest != "" {
				req["memory"] = r.MemoryRequest
			}
			resources["requests"] = req
		}
		if lim := map[string]string{}; r.CPULimit != "" || r.MemoryLimit != "" {
			if r.CPULimit != "" {
				lim["cpu"] = r.CPULimit
			}
			if r.MemoryLimit != "" {
				lim["memory"] = r.MemoryLimit
			}
			resources["limits"] = lim
		}
		for k, v := range r.ExtendedResources {
			parts := strings.SplitN(k, ".", 2)
			if len(parts) != 2 {
				continue
			}
			category, resourceName := parts[0], parts[1]
			switch category {
			case "limits":
				if resources["limits"] == nil {
					resources["limits"] = map[string]string{}
				}
				resources["limits"].(map[string]string)[resourceName] = v
			case "requests":
				if resources["requests"] == nil {
					resources["requests"] = map[string]string{}
				}
				resources["requests"].(map[string]string)[resourceName] = v
			}
		}
		containerSpec["resources"] = resources
	}
	items = append(items, map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      deployment.Name,
			"namespace": deployment.Namespace,
			"labels":    cloneStringMap(deployment.Labels),
			"annotations": map[string]string{
				fullHashAnnotation: deployment.Hash,
			},
		},
		"spec": map[string]any{
			"replicas": replicas,
			"selector": map[string]any{
				"matchLabels": selector,
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": cloneStringMap(deployment.Labels),
				},
				"spec": func() map[string]any {
					podSpec := map[string]any{
						"containers": []map[string]any{containerSpec},
						"volumes":    volumeItems,
					}
					if deployment.ServiceAccountName != "" {
						podSpec["serviceAccountName"] = deployment.ServiceAccountName
					}
					if deployment.HostUsers != nil {
						podSpec["hostUsers"] = *deployment.HostUsers
					}
					return podSpec
				}(),
			},
		},
	})
	return items
}

func listManifest(items []map[string]any) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "List",
		"items":      items,
	}
}

func containerVolumes(deployment Deployment) ([]map[string]any, []map[string]any) {
	volumes := make([]map[string]any, 0, len(deployment.Container.VolumeMounts)+1)
	mounts := make([]map[string]any, 0, len(deployment.Container.VolumeMounts))
	inlineName := inlineConfigMapName(deployment.Name)
	for idx, mount := range deployment.Container.VolumeMounts {
		name := fmt.Sprintf("vol-%d", idx)
		volume := map[string]any{"name": name}
		switch mount.SourceKind {
		case "ConfigMap":
			volume["configMap"] = map[string]any{"name": mount.SourceName}
		case "PersistentVolumeClaim":
			volume["persistentVolumeClaim"] = map[string]any{"claimName": mount.SourceName}
		case "HostPath":
			volume["hostPath"] = map[string]any{"path": mount.SourceName}
		case "EmptyDir":
			volume["emptyDir"] = map[string]any{}
		case "InlineFile":
			volume["configMap"] = map[string]any{"name": inlineName}
		default:
			continue
		}
		volumes = append(volumes, volume)
		mountSpec := map[string]any{
			"name":      name,
			"mountPath": mount.MountPath,
			"readOnly":  mount.ReadOnly,
		}
		if mount.SubPath != "" {
			mountSpec["subPath"] = mount.SubPath
		} else if mount.SourceKind == "InlineFile" && mount.SourceName != "" {
			mountSpec["subPath"] = mount.SourceName
		}
		mounts = append(mounts, mountSpec)
	}
	return volumes, mounts
}
