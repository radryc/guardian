package drivers

func stringProperty(props map[string]any, key string) (string, bool) {
	if props == nil {
		return "", false
	}
	value, ok := props[key]
	if !ok {
		return "", false
	}
	result, ok := value.(string)
	return result, ok
}
