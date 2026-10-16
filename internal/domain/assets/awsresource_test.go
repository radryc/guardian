package assets

import (
	"strings"
	"testing"
)

func TestAWSResourceValidate(t *testing.T) {
	valid := &AWSResourceSpec{
		CfnType:    "AWS::SQS::Queue",
		Properties: map[string]any{"QueueName": "guardian-queue"},
		Outputs:    map[string]string{"queueName": "guardian-queue"},
	}
	if err := (awsResourceDefinition{}).Validate(valid, ValidationContext{}); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}

	missingType := &AWSResourceSpec{}
	if err := (awsResourceDefinition{}).Validate(missingType, ValidationContext{}); err == nil {
		t.Fatal("Validate(missingType) expected error")
	}

	badType := &AWSResourceSpec{CfnType: "SQSQueue"}
	if err := (awsResourceDefinition{}).Validate(badType, ValidationContext{}); err == nil ||
		!strings.Contains(err.Error(), "AWS::<Service>::<Type>") {
		t.Fatalf("Validate(badType) error = %v, want type format error", err)
	}

	emptyOutputKey := &AWSResourceSpec{
		CfnType: "AWS::SQS::Queue",
		Outputs: map[string]string{"": "x"},
	}
	if err := (awsResourceDefinition{}).Validate(emptyOutputKey, ValidationContext{}); err == nil {
		t.Fatal("Validate(emptyOutputKey) expected error")
	}
}

func TestAWSResourceRegisteredAndDecoded(t *testing.T) {
	def, ok := DefinitionFor("AWSResource")
	if !ok {
		t.Fatal("AWSResource definition not registered")
	}
	if def.Type() != "AWSResource" {
		t.Fatalf("type = %q", def.Type())
	}
	if _, ok := def.NewSpec().(*AWSResourceSpec); !ok {
		t.Fatalf("NewSpec() = %T", def.NewSpec())
	}
}

func TestAWSResourceExistingFlag(t *testing.T) {
	spec := &AWSResourceSpec{CfnType: "AWS::SQS::Queue", Existing: true}
	if err := (awsResourceDefinition{}).Validate(spec, ValidationContext{}); err != nil {
		t.Fatalf("Validate(existing) error = %v", err)
	}
}
