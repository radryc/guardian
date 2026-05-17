package format

type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

type MutationResult struct {
	Success         bool   `json:"success"`
	LogicalPath     string `json:"logicalPath"`
	VersionID       string `json:"versionID"`
	BatchRevisionID string `json:"batchRevisionID"`
	CorrelationID   string `json:"correlationID"`
}
