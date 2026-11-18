package target

import (
	"encoding/json"
	"testing"
)

func TestPlacementZeroValueAndJSON(t *testing.T) {
	var zero Placement
	if zero.Cluster != "" || zero.Namespace != "" || zero.Region != "" || zero.Account != "" {
		t.Fatalf("zero value = %+v, want empty placement", zero)
	}

	input := Placement{Cluster: "main", Namespace: "platform", Region: "eu-west-1", Account: "123456789012"}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Placement
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != input {
		t.Fatalf("round-trip = %+v, want %+v", decoded, input)
	}
}
