package task

import "strings"

// RedactedValue is the placeholder written instead of secret material.
const RedactedValue = "******"

// SecretAssetNames returns the names of Secret assets in the task.
func (t *Task) SecretAssetNames() []string {
	if t == nil {
		return nil
	}
	names := make([]string, 0, 1)
	for _, asset := range t.Assets {
		if strings.EqualFold(strings.TrimSpace(asset.Type), "Secret") {
			names = append(names, asset.Name)
		}
	}
	return names
}

// RedactSecretOutputs masks plaintext secret values in an output map while
// preserving valueHash (used for drift detection) and secretRef. It returns a
// copy so callers do not mutate shared maps.
func RedactSecretOutputs(t *Task, outputs map[string]string) map[string]string {
	if len(outputs) == 0 {
		return outputs
	}
	secrets := t.SecretAssetNames()
	if len(secrets) == 0 {
		return outputs
	}
	redacted := make(map[string]string, len(outputs))
	for key, value := range outputs {
		redacted[key] = value
	}
	secretSet := make(map[string]bool, len(secrets))
	for _, name := range secrets {
		secretSet[name] = true
	}
	if _, ok := redacted["value"]; ok {
		redacted["value"] = RedactedValue
	}
	for key := range redacted {
		if !strings.HasSuffix(key, ".value") {
			continue
		}
		owner := strings.TrimSuffix(key, ".value")
		if secretSet[owner] {
			redacted[key] = RedactedValue
		}
	}
	return redacted
}

// RedactSecretStrings replaces every occurrence of the supplied secret values
// in a set of strings. Empty values are ignored.
func RedactSecretStrings(values []string, secrets []string) []string {
	if len(values) == 0 || len(secrets) == 0 {
		return values
	}
	out := make([]string, len(values))
	for i, value := range values {
		for _, secret := range secrets {
			if secret == "" {
				continue
			}
			value = strings.ReplaceAll(value, secret, RedactedValue)
		}
		out[i] = value
	}
	return out
}
