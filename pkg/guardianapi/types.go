package guardianapi

import (
	"context"
	"errors"
	"time"
)

var ErrConflict = errors.New("guardianapi: version conflict")

// ReadStore provides read-only access to the logical filesystem.
type ReadStore interface {
	ReadFile(ctx context.Context, logicalPath string) ([]byte, error)
	ListDir(ctx context.Context, logicalDir string) ([]DirEntry, error)
	Stat(ctx context.Context, logicalPath string) (FileInfo, error)
}

type DirListOptions struct {
	Offset int
	Limit  int
}

type DirListPage struct {
	Entries    []DirEntry `json:"entries"`
	NextOffset int        `json:"nextOffset"`
	HasMore    bool       `json:"hasMore"`
}

type PagedDirLister interface {
	ListDirPage(ctx context.Context, logicalDir string, opts DirListOptions) (DirListPage, error)
}

// WatchStore provides change notifications for logical path prefixes.
type WatchStore interface {
	Watch(ctx context.Context, prefixes []string) (<-chan ChangeEvent, error)
}

// WriteStore provides mutation and versioning operations over logical paths.
type WriteStore interface {
	UpsertFiles(ctx context.Context, batch MutationBatch) (BatchRevision, error)
	DeletePaths(ctx context.Context, batch DeleteBatch) (BatchRevision, error)
	ListVersions(ctx context.Context, logicalPath string) ([]FileVersion, error)
	GetVersion(ctx context.Context, logicalPath, versionID string) (VersionedFile, error)
}

// Store is the complete Guardian storage contract.
type Store interface {
	ReadStore
	WatchStore
	WriteStore
}

type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

type FileInfo struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	VersionID string    `json:"versionID"`
	ModTime   time.Time `json:"modTime"`
}

type ChangeType string

const (
	ChangeAdded    ChangeType = "Added"
	ChangeModified ChangeType = "Modified"
	ChangeDeleted  ChangeType = "Deleted"
)

type ChangeEvent struct {
	LogicalPath     string     `json:"logicalPath"`
	Type            ChangeType `json:"type"`
	VersionID       string     `json:"versionID"`
	BatchRevisionID string     `json:"batchRevisionID"`
	CommittedAt     time.Time  `json:"committedAt"`
}

type MutationBatch struct {
	Writes  []PathWrite     `json:"writes"`
	Context MutationContext `json:"context"`
}

type PathWrite struct {
	LogicalPath       string `json:"logicalPath"`
	Content           []byte `json:"content"`
	ExpectedVersionID string `json:"expectedVersionID"`
}

type DeleteBatch struct {
	Deletes []PathDelete    `json:"deletes"`
	Context MutationContext `json:"context"`
}

type PathDelete struct {
	LogicalPath       string `json:"logicalPath"`
	ExpectedVersionID string `json:"expectedVersionID"`
}

type MutationContext struct {
	PrincipalID   string `json:"principalID"`
	Reason        string `json:"reason"`
	CorrelationID string `json:"correlationID"`
}

type BatchRevision struct {
	BatchRevisionID string        `json:"batchRevisionID"`
	Files           []FileVersion `json:"files"`
}

type FileVersion struct {
	LogicalPath     string    `json:"logicalPath"`
	VersionID       string    `json:"versionID"`
	BatchRevisionID string    `json:"batchRevisionID"`
	ContentSHA256   string    `json:"contentSHA256"`
	CommittedAt     time.Time `json:"committedAt"`
	Tombstone       bool      `json:"tombstone"`
	PrincipalID     string    `json:"principalID"`
	Reason          string    `json:"reason"`
}

type VersionedFile struct {
	Version FileVersion `json:"version"`
	Content []byte      `json:"content"`
}
