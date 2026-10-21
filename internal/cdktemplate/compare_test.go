package cdktemplate

import "testing"

const yamlTemplate = `AWSTemplateFormatVersion: '2010-09-09'
Resources:
  Bucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: demo-bucket
`

const jsonTemplate = `{"AWSTemplateFormatVersion":"2010-09-09","Resources":{"Bucket":{"Type":"AWS::S3::Bucket","Properties":{"BucketName":"demo-bucket"}}}}`

func TestCompareAcceptsYAMLAndJSON(t *testing.T) {
	changed, err := Compare(jsonTemplate, yamlTemplate)
	if err != nil {
		t.Fatalf("Compare(json, yaml): %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("equivalent templates differ: %v", changed)
	}
	changed, err = Compare(jsonTemplate, `{"Resources":{"Bucket":{"Type":"AWS::S3::Bucket","Properties":{"BucketName":"other"}}}}`)
	if err != nil {
		t.Fatalf("Compare(json, json): %v", err)
	}
	if len(changed) != 1 {
		t.Fatalf("changed = %v, want one resource", changed)
	}
}

func TestHashNormalizesEncodings(t *testing.T) {
	if Hash(yamlTemplate) != Hash(jsonTemplate) {
		t.Fatal("YAML and JSON encodings of the same template must hash equally")
	}
}

func TestCompareExpandsShortFormIntrinsics(t *testing.T) {
	const yamlWithRef = `Resources:
  Bucket:
    Type: AWS::S3::Bucket
  Policy:
    Type: AWS::S3::BucketPolicy
    Properties:
      Bucket: !Ref Bucket
      Arn: !GetAtt Bucket.Arn
`
	const jsonWithRef = `{"Resources":{"Bucket":{"Type":"AWS::S3::Bucket"},"Policy":{"Type":"AWS::S3::BucketPolicy","Properties":{"Bucket":{"Ref":"Bucket"},"Arn":{"Fn::GetAtt":["Bucket","Arn"]}}}}}`
	changed, err := Compare(jsonWithRef, yamlWithRef)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("short-form intrinsics differ: %v", changed)
	}
	if Hash(jsonWithRef) != Hash(yamlWithRef) {
		t.Fatal("short-form YAML and long-form JSON must hash equally")
	}
}
