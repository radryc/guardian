package assets

import (
	"encoding/json"
	"fmt"
	"strings"
)

type StringList []string

func (s *StringList) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = nil
		return nil
	}

	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		if strings.TrimSpace(single) == "" {
			return fmt.Errorf("must not be empty")
		}
		*s = []string{single}
		return nil
	}

	var many []string
	if err := json.Unmarshal(data, &many); err == nil {
		for idx, item := range many {
			if strings.TrimSpace(item) == "" {
				return fmt.Errorf("entry %d must not be empty", idx)
			}
		}
		*s = many
		return nil
	}

	return fmt.Errorf("must be a string or list of strings")
}

func requireString(value, field string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("property %s is required", field)
	}
	return nil
}

func requirePositiveInt(value *int, field string) error {
	if value == nil {
		return fmt.Errorf("property %s is required", field)
	}
	if *value <= 0 {
		return fmt.Errorf("property %s must be a positive integer", field)
	}
	return nil
}

func optionalPositiveInt(value *int, field string) error {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return fmt.Errorf("property %s must be a positive integer", field)
	}
	return nil
}

func requireAbsolutePath(pathValue, field string) error {
	if err := requireString(pathValue, field); err != nil {
		return err
	}
	if !strings.HasPrefix(pathValue, "/") {
		return fmt.Errorf("property %s must be an absolute path", field)
	}
	return nil
}

func validateAssetRef(ctx ValidationContext, refName, wantType, field string) error {
	if err := requireString(refName, field); err != nil {
		return err
	}
	if ctx.AssetTypes[refName] != wantType {
		return fmt.Errorf("property %s must reference an existing %s asset", field, wantType)
	}
	return nil
}

func validateStringList(values []string, field string) error {
	for idx, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("property %s[%d] must be a non-empty string", field, idx)
		}
	}
	return nil
}

func assetError(name, assetType string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("asset %q (%s): %w", name, assetType, err)
}
