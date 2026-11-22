package bundle

import (
	"fmt"

	"github.com/radryc/guardian/internal/compiler/planner"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	"gopkg.in/yaml.v3"
)

// resolveIntentValues applies ${value.NAME} substitutions before the intent is
// normalized and typed asset validation runs. Without this, a placeholder in a
// numeric field would fail decoding before the planner can resolve it.
func resolveIntentValues(partitionValues map[string]any, content []byte) ([]byte, error) {
	var intent intentdomain.Intent
	if err := yaml.Unmarshal(content, &intent); err == nil {
		if err := planner.ApplyValues(partitionValues, &intent); err != nil {
			return nil, err
		}
		return yaml.Marshal(intent)
	}
	var list []intentdomain.Intent
	if err := yaml.Unmarshal(content, &list); err != nil {
		// Let the normalizer report the original parse error.
		return content, nil
	}
	if len(list) == 0 {
		return content, nil
	}
	if err := planner.ApplyValues(partitionValues, &list[0]); err != nil {
		return nil, err
	}
	return yaml.Marshal(list)
}

func resolveIntentFileValues(partitionValues map[string]any, files []IntentFile) error {
	for i := range files {
		content, err := resolveIntentValues(partitionValues, files[i].Content)
		if err != nil {
			return fmt.Errorf("%s: %w", files[i].Path, err)
		}
		files[i].Content = content
	}
	return nil
}
