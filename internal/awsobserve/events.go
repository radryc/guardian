package awsobserve

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	detailTypeStackStatus    = "CloudFormation Stack Status Change"
	detailTypeDriftDetection = "CloudFormation Drift Detection Status Change"
)

type eventEnvelope struct {
	ID         string          `json:"id"`
	DetailType string          `json:"detail-type"`
	Source     string          `json:"source"`
	Account    string          `json:"account"`
	Region     string          `json:"region"`
	Time       time.Time       `json:"time"`
	Resources  []string        `json:"resources"`
	Detail     json.RawMessage `json:"detail"`
}

type stackStatusDetail struct {
	StackID       string `json:"stack-id"`
	StatusDetails struct {
		Status       string `json:"status"`
		StatusReason string `json:"status-reason"`
	} `json:"status-details"`
}

type driftDetectionDetail struct {
	StackID       string `json:"stack-id"`
	StatusDetails struct {
		StackDriftStatus string `json:"stack-drift-status"`
		DetectionStatus  string `json:"detection-status"`
	} `json:"status-details"`
	DriftDetectionDetails struct {
		DriftedStackResourceCount int `json:"drifted-stack-resource-count"`
	} `json:"drift-detection-details"`
}

// ParseEvent normalizes one EventBridge CloudFormation event. Unsupported
// detail types return ok=false.
func ParseEvent(body []byte) (Event, bool, error) {
	var envelope eventEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Event{}, false, fmt.Errorf("parse eventbridge envelope: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(envelope.Source), "aws.cloudformation") {
		return Event{}, false, nil
	}
	ev := Event{
		ID:         envelope.ID,
		Account:    envelope.Account,
		Region:     envelope.Region,
		DetailType: envelope.DetailType,
		Time:       envelope.Time,
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	switch envelope.DetailType {
	case detailTypeStackStatus:
		var detail stackStatusDetail
		if err := json.Unmarshal(envelope.Detail, &detail); err != nil {
			return Event{}, false, fmt.Errorf("parse stack status detail: %w", err)
		}
		ev.StackID = detail.StackID
		ev.Status = detail.StatusDetails.Status
		ev.StatusReason = detail.StatusDetails.StatusReason
	case detailTypeDriftDetection:
		var detail driftDetectionDetail
		if err := json.Unmarshal(envelope.Detail, &detail); err != nil {
			return Event{}, false, fmt.Errorf("parse drift detection detail: %w", err)
		}
		ev.StackID = detail.StackID
		ev.DriftStatus = detail.StatusDetails.StackDriftStatus
		ev.DetectionStatus = detail.StatusDetails.DetectionStatus
		ev.DriftedResourceCount = detail.DriftDetectionDetails.DriftedStackResourceCount
	default:
		return Event{}, false, nil
	}
	ev.StackName = StackNameFromARN(ev.StackID)
	if ev.StackName == "" && len(envelope.Resources) > 0 {
		ev.StackName = StackNameFromARN(envelope.Resources[0])
	}
	if ev.StackName == "" {
		return Event{}, false, fmt.Errorf("event %s has no resolvable stack name", ev.ID)
	}
	return ev, true, nil
}

// StackNameFromARN extracts the stack name from a CloudFormation stack ARN or
// resource ARN, e.g. arn:aws:cloudformation:eu-west-1:123:stack/name/guid.
func StackNameFromARN(arn string) string {
	arn = strings.TrimSpace(arn)
	if arn == "" {
		return ""
	}
	idx := strings.Index(arn, ":stack/")
	if idx < 0 {
		if strings.HasPrefix(arn, "stack/") {
			arn = arn[len("stack/"):]
		} else {
			return ""
		}
	} else {
		arn = arn[idx+len(":stack/"):]
	}
	if slash := strings.Index(arn, "/"); slash >= 0 {
		arn = arn[:slash]
	}
	return strings.TrimSpace(arn)
}
