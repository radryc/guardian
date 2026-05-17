package fs

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type Store struct {
	root string
	mu   sync.Mutex
}

type fileMeta struct {
	Current string                    `json:"current"`
	History []guardianapi.FileVersion `json:"history"`
}

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, ".versions", "meta"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, ".versions", "data"), 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) ReadFile(ctx context.Context, logicalPath string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return os.ReadFile(s.logicalToPhysical(logicalPath))
}

func (s *Store) ListDir(ctx context.Context, logicalDir string) ([]guardianapi.DirEntry, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	physical := s.logicalToPhysical(logicalDir)
	entries, err := os.ReadDir(physical)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]guardianapi.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if logicalDir == "/" && entry.Name() == ".versions" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, guardianapi.DirEntry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Size:  info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) ListDirPage(ctx context.Context, logicalDir string, opts guardianapi.DirListOptions) (guardianapi.DirListPage, error) {
	entries, err := s.ListDir(ctx, logicalDir)
	if err != nil {
		return guardianapi.DirListPage{}, err
	}
	return paginateDirEntries(entries, opts)
}

func paginateDirEntries(entries []guardianapi.DirEntry, opts guardianapi.DirListOptions) (guardianapi.DirListPage, error) {
	if opts.Offset < 0 {
		return guardianapi.DirListPage{}, fmt.Errorf("offset must be zero or greater")
	}
	if opts.Limit < 0 {
		return guardianapi.DirListPage{}, fmt.Errorf("limit must be zero or greater")
	}
	if opts.Offset >= len(entries) {
		return guardianapi.DirListPage{Entries: nil, NextOffset: opts.Offset, HasMore: false}, nil
	}
	end := len(entries)
	if opts.Limit > 0 && opts.Offset+opts.Limit < end {
		end = opts.Offset + opts.Limit
	}
	page := guardianapi.DirListPage{
		Entries: append([]guardianapi.DirEntry(nil), entries[opts.Offset:end]...),
	}
	if end < len(entries) {
		page.HasMore = true
		page.NextOffset = end
	} else {
		page.NextOffset = end
	}
	return page, nil
}

func (s *Store) Stat(ctx context.Context, logicalPath string) (guardianapi.FileInfo, error) {
	select {
	case <-ctx.Done():
		return guardianapi.FileInfo{}, ctx.Err()
	default:
	}
	physical := s.logicalToPhysical(logicalPath)
	info, err := os.Stat(physical)
	if err != nil {
		return guardianapi.FileInfo{}, err
	}
	meta, err := s.loadMeta(logicalPath)
	if err != nil {
		return guardianapi.FileInfo{}, err
	}
	versionID := ""
	if meta != nil {
		versionID = meta.Current
	}
	return guardianapi.FileInfo{
		Path:      normalizeLogicalPath(logicalPath),
		Size:      info.Size(),
		VersionID: versionID,
		ModTime:   info.ModTime().UTC(),
	}, nil
}

func (s *Store) UpsertFiles(ctx context.Context, batch guardianapi.MutationBatch) (guardianapi.BatchRevision, error) {
	select {
	case <-ctx.Done():
		return guardianapi.BatchRevision{}, ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, write := range batch.Writes {
		meta, err := s.loadMeta(write.LogicalPath)
		if err != nil {
			return guardianapi.BatchRevision{}, err
		}
		current := ""
		if meta != nil {
			current = meta.Current
		}
		if err := checkExpected(current, write.ExpectedVersionID); err != nil {
			return guardianapi.BatchRevision{}, fmt.Errorf("write %s: %w", write.LogicalPath, err)
		}
	}

	now := time.Now().UTC()
	result := guardianapi.BatchRevision{BatchRevisionID: revisions.NewBatchRevisionID()}
	for _, write := range batch.Writes {
		logicalPath := normalizeLogicalPath(write.LogicalPath)
		version := guardianapi.FileVersion{
			LogicalPath:     logicalPath,
			VersionID:       revisions.NewVersionID(),
			BatchRevisionID: result.BatchRevisionID,
			ContentSHA256:   digest.ContentHash(write.Content),
			CommittedAt:     now,
			PrincipalID:     batch.Context.PrincipalID,
			Reason:          batch.Context.Reason,
		}
		if err := os.MkdirAll(filepath.Dir(s.logicalToPhysical(logicalPath)), 0o755); err != nil {
			return guardianapi.BatchRevision{}, err
		}
		if err := os.WriteFile(s.logicalToPhysical(logicalPath), write.Content, 0o644); err != nil {
			return guardianapi.BatchRevision{}, err
		}
		if err := os.MkdirAll(s.versionDataDir(logicalPath), 0o755); err != nil {
			return guardianapi.BatchRevision{}, err
		}
		if err := os.WriteFile(filepath.Join(s.versionDataDir(logicalPath), version.VersionID), write.Content, 0o644); err != nil {
			return guardianapi.BatchRevision{}, err
		}
		meta, err := s.loadMeta(logicalPath)
		if err != nil {
			return guardianapi.BatchRevision{}, err
		}
		if meta == nil {
			meta = &fileMeta{}
		}
		meta.Current = version.VersionID
		meta.History = append(meta.History, version)
		if err := s.storeMeta(logicalPath, meta); err != nil {
			return guardianapi.BatchRevision{}, err
		}
		result.Files = append(result.Files, version)
	}
	return result, nil
}

func (s *Store) DeletePaths(ctx context.Context, batch guardianapi.DeleteBatch) (guardianapi.BatchRevision, error) {
	select {
	case <-ctx.Done():
		return guardianapi.BatchRevision{}, ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, del := range batch.Deletes {
		meta, err := s.loadMeta(del.LogicalPath)
		if err != nil {
			return guardianapi.BatchRevision{}, err
		}
		current := ""
		if meta != nil {
			current = meta.Current
		}
		if err := checkExpected(current, del.ExpectedVersionID); err != nil {
			return guardianapi.BatchRevision{}, fmt.Errorf("delete %s: %w", del.LogicalPath, err)
		}
	}

	now := time.Now().UTC()
	result := guardianapi.BatchRevision{BatchRevisionID: revisions.NewBatchRevisionID()}
	for _, del := range batch.Deletes {
		logicalPath := normalizeLogicalPath(del.LogicalPath)
		version := guardianapi.FileVersion{
			LogicalPath:     logicalPath,
			VersionID:       revisions.NewVersionID(),
			BatchRevisionID: result.BatchRevisionID,
			ContentSHA256:   digest.ContentHash(nil),
			CommittedAt:     now,
			Tombstone:       true,
			PrincipalID:     batch.Context.PrincipalID,
			Reason:          batch.Context.Reason,
		}
		_ = os.Remove(s.logicalToPhysical(logicalPath))
		meta, err := s.loadMeta(logicalPath)
		if err != nil {
			return guardianapi.BatchRevision{}, err
		}
		if meta == nil {
			meta = &fileMeta{}
		}
		meta.Current = version.VersionID
		meta.History = append(meta.History, version)
		if err := s.storeMeta(logicalPath, meta); err != nil {
			return guardianapi.BatchRevision{}, err
		}
		result.Files = append(result.Files, version)
	}
	return result, nil
}

func (s *Store) ListVersions(ctx context.Context, logicalPath string) ([]guardianapi.FileVersion, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	meta, err := s.loadMeta(logicalPath)
	if err != nil || meta == nil {
		return nil, err
	}
	out := make([]guardianapi.FileVersion, 0, len(meta.History))
	for i := len(meta.History) - 1; i >= 0; i-- {
		out = append(out, meta.History[i])
	}
	return out, nil
}

func (s *Store) GetVersion(ctx context.Context, logicalPath, versionID string) (guardianapi.VersionedFile, error) {
	select {
	case <-ctx.Done():
		return guardianapi.VersionedFile{}, ctx.Err()
	default:
	}
	meta, err := s.loadMeta(logicalPath)
	if err != nil || meta == nil {
		return guardianapi.VersionedFile{}, err
	}
	for _, version := range meta.History {
		if version.VersionID != versionID {
			continue
		}
		var content []byte
		if !version.Tombstone {
			content, err = os.ReadFile(filepath.Join(s.versionDataDir(logicalPath), versionID))
			if err != nil {
				return guardianapi.VersionedFile{}, err
			}
		}
		return guardianapi.VersionedFile{Version: version, Content: content}, nil
	}
	return guardianapi.VersionedFile{}, os.ErrNotExist
}

func (s *Store) Watch(ctx context.Context, prefixes []string) (<-chan guardianapi.ChangeEvent, error) {
	return nil, fmt.Errorf("filesystem store watch not supported")
}

func (s *Store) logicalToPhysical(logicalPath string) string {
	clean := normalizeLogicalPath(logicalPath)
	trimmed := strings.TrimPrefix(clean, "/")
	if trimmed == "" {
		return s.root
	}
	return filepath.Join(s.root, filepath.FromSlash(trimmed))
}

func normalizeLogicalPath(logicalPath string) string {
	clean := path.Clean(logicalPath)
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return clean
}

func (s *Store) metaPath(logicalPath string) string {
	return filepath.Join(s.root, ".versions", "meta", hashPath(logicalPath)+".json")
}

func (s *Store) versionDataDir(logicalPath string) string {
	return filepath.Join(s.root, ".versions", "data", hashPath(logicalPath))
}

func hashPath(logicalPath string) string {
	return hex.EncodeToString([]byte(normalizeLogicalPath(logicalPath)))
}

func (s *Store) loadMeta(logicalPath string) (*fileMeta, error) {
	data, err := os.ReadFile(s.metaPath(logicalPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var meta fileMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *Store) storeMeta(logicalPath string, meta *fileMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(logicalPath), data, 0o644)
}

func checkExpected(current, expected string) error {
	switch expected {
	case "":
		return nil
	case "absent":
		if current != "" {
			return guardianapi.ErrConflict
		}
		return nil
	default:
		if current != expected {
			return guardianapi.ErrConflict
		}
		return nil
	}
}

var _ guardianapi.Store = (*Store)(nil)
