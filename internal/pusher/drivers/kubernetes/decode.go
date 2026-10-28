package kubernetesdriver

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Shared decoders translate raw Kubernetes JSON into Guardian's internal types.
// Both the kubectl and client-go backends feed identically shaped JSON here.

func decodeConfigMap(namespace string, raw []byte) (ConfigMap, error) {
	var payload struct {
		Metadata struct {
			Name        string            `json:"name"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Data       map[string]string `json:"data"`
		BinaryData map[string]string `json:"binaryData"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ConfigMap{}, fmt.Errorf("decode configmap: %w", err)
	}
	hash := payload.Metadata.Annotations[fullHashAnnotation]
	if hash == "" {
		hash = payload.Metadata.Labels["guardian.hash"]
	}
	binaryData := make(map[string]string, len(payload.BinaryData))
	for name, encoded := range payload.BinaryData {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return ConfigMap{}, fmt.Errorf("decode configmap binaryData[%s]: %w", name, err)
		}
		binaryData[name] = string(decoded)
	}
	return ConfigMap{
		Namespace:  namespace,
		Name:       payload.Metadata.Name,
		Hash:       hash,
		Labels:     cloneStringMap(payload.Metadata.Labels),
		Data:       cloneStringMap(payload.Data),
		BinaryData: binaryData,
	}, nil
}

func decodeClaim(namespace string, raw []byte) (PersistentVolumeClaim, error) {
	var payload struct {
		Metadata struct {
			Name        string            `json:"name"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			AccessModes      []string `json:"accessModes"`
			StorageClassName string   `json:"storageClassName"`
			Resources        struct {
				Requests map[string]string `json:"requests"`
			} `json:"resources"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return PersistentVolumeClaim{}, fmt.Errorf("decode pvc: %w", err)
	}
	accessMode := ""
	if len(payload.Spec.AccessModes) > 0 {
		accessMode = payload.Spec.AccessModes[0]
	}
	hash := payload.Metadata.Annotations[fullHashAnnotation]
	if hash == "" {
		hash = payload.Metadata.Labels["guardian.hash"]
	}
	return PersistentVolumeClaim{
		Namespace:    namespace,
		Name:         payload.Metadata.Name,
		Hash:         hash,
		Labels:       cloneStringMap(payload.Metadata.Labels),
		Size:         payload.Spec.Resources.Requests["storage"],
		AccessMode:   accessMode,
		StorageClass: payload.Spec.StorageClassName,
	}, nil
}

func decodeDeployment(namespace string, raw []byte) (Deployment, error) {
	var payload struct {
		Metadata struct {
			Name        string            `json:"name"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			Replicas *int `json:"replicas"`
			Template struct {
				Spec struct {
					Containers []struct {
						Name            string `json:"name"`
						Image           string `json:"image"`
						ImagePullPolicy string `json:"imagePullPolicy"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
		Status struct {
			ReadyReplicas     int `json:"readyReplicas"`
			AvailableReplicas int `json:"availableReplicas"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Deployment{}, fmt.Errorf("decode deployment: %w", err)
	}
	container := Container{}
	if len(payload.Spec.Template.Spec.Containers) > 0 {
		container.Name = payload.Spec.Template.Spec.Containers[0].Name
		container.Image = payload.Spec.Template.Spec.Containers[0].Image
		container.ImagePullPolicy = payload.Spec.Template.Spec.Containers[0].ImagePullPolicy
	}
	replicas := 1
	if payload.Spec.Replicas != nil && *payload.Spec.Replicas > 0 {
		replicas = *payload.Spec.Replicas
	}
	hash := payload.Metadata.Annotations[fullHashAnnotation]
	if hash == "" {
		hash = payload.Metadata.Labels["guardian.hash"]
	}
	return Deployment{
		Namespace:         namespace,
		Name:              payload.Metadata.Name,
		Hash:              hash,
		Labels:            cloneStringMap(payload.Metadata.Labels),
		Replicas:          replicas,
		ReadyReplicas:     payload.Status.ReadyReplicas,
		AvailableReplicas: payload.Status.AvailableReplicas,
		Container:         container,
	}, nil
}

func decodeService(namespace string, raw []byte) (Service, error) {
	var payload struct {
		Metadata struct {
			Name        string            `json:"name"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			Type     string            `json:"type"`
			Selector map[string]string `json:"selector"`
			Ports    []struct {
				Name       string      `json:"name"`
				Protocol   string      `json:"protocol"`
				Port       int         `json:"port"`
				TargetPort interface{} `json:"targetPort"`
			} `json:"ports"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Service{}, fmt.Errorf("decode service: %w", err)
	}
	ports := make([]ServicePort, 0, len(payload.Spec.Ports))
	for _, port := range payload.Spec.Ports {
		ports = append(ports, ServicePort{
			Name:       port.Name,
			Protocol:   port.Protocol,
			Port:       port.Port,
			TargetPort: parseTargetPort(port.TargetPort, port.Port),
		})
	}
	hash := payload.Metadata.Annotations[fullHashAnnotation]
	if hash == "" {
		hash = payload.Metadata.Labels["guardian.hash"]
	}
	return Service{
		Namespace:   namespace,
		Name:        payload.Metadata.Name,
		Hash:        hash,
		Type:        payload.Spec.Type,
		Labels:      cloneStringMap(payload.Metadata.Labels),
		Annotations: cloneStringMap(payload.Metadata.Annotations),
		Selector:    cloneStringMap(payload.Spec.Selector),
		Ports:       ports,
	}, nil
}

func decodeNetworkPolicy(namespace string, raw []byte) (NetworkPolicy, error) {
	var payload struct {
		Metadata struct {
			Name        string            `json:"name"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			PodSelector struct {
				MatchLabels map[string]string `json:"matchLabels"`
			} `json:"podSelector"`
			PolicyTypes []string `json:"policyTypes"`
			Ingress     []struct {
				From []struct {
					NamespaceSelector *struct {
						MatchLabels map[string]string `json:"matchLabels"`
					} `json:"namespaceSelector"`
				} `json:"from"`
			} `json:"ingress"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return NetworkPolicy{}, fmt.Errorf("decode network policy: %w", err)
	}
	hash := payload.Metadata.Annotations[fullHashAnnotation]
	if hash == "" {
		hash = payload.Metadata.Labels["guardian.hash"]
	}
	allowCluster := false
	for _, rule := range payload.Spec.Ingress {
		for _, from := range rule.From {
			if from.NamespaceSelector != nil && len(from.NamespaceSelector.MatchLabels) == 0 {
				allowCluster = true
			}
		}
	}
	internal := false
	for _, policyType := range payload.Spec.PolicyTypes {
		if policyType == "Egress" {
			internal = true
		}
	}
	return NetworkPolicy{
		Namespace:    namespace,
		Name:         payload.Metadata.Name,
		Hash:         hash,
		Labels:       cloneStringMap(payload.Metadata.Labels),
		PodSelector:  cloneStringMap(payload.Spec.PodSelector.MatchLabels),
		AllowCluster: allowCluster,
		Internal:     internal,
	}, nil
}
