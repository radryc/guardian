package assets

import "testing"

func TestSecretValidate(t *testing.T) {
	validValue := &SecretSpec{Value: "super-secret"}
	if err := (secretDefinition{}).Validate(validValue, ValidationContext{}); err != nil {
		t.Fatalf("Validate(validValue) error = %v", err)
	}

	validRef := &SecretSpec{SecretRef: "monofs-secret://shared/encryption-key"}
	if err := (secretDefinition{}).Validate(validRef, ValidationContext{}); err != nil {
		t.Fatalf("Validate(validRef) error = %v", err)
	}

	missing := &SecretSpec{}
	if err := (secretDefinition{}).Validate(missing, ValidationContext{}); err == nil {
		t.Fatal("Validate(missing) expected error")
	}

	both := &SecretSpec{Value: "a", SecretRef: "monofs-secret://shared/key"}
	if err := (secretDefinition{}).Validate(both, ValidationContext{}); err == nil {
		t.Fatal("Validate(both) expected error")
	}
}
