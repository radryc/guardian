package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type Store struct {
	mu        sync.RWMutex
	files     map[string]*fileRecord
	watchers  map[int]watcherRegistration
	nextWatch int
}

type fileRecord struct {
	current *versionRecord
	history []versionRecord
}

type versionRecord struct {
	version guardianapi.FileVersion
	content []byte
}

type watcherRegistration struct {
	prefixes []string
	ch       chan guardianapi.ChangeEvent
}

func New() *Store {
	return &Store{
		files:    map[string]*fileRecord{},
		watchers: map[int]watcherRegistration{},
	}
}

func normalizeLogicalPath(logicalPath string) string {
	if logicalPath == "" {
		return "/"
	}
	clean := path.Clean(logicalPath)
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return clean
}

func copyBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func (s *Store) ReadFile(ctx context.Context, logicalPath string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	logicalPath = normalizeLogicalPath(logicalPath)
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.files[logicalPath]
	if !ok || rec.current == nil || rec.current.version.Tombstone {
		return nil, os.ErrNotExist
	}
	return copyBytes(rec.current.content), nil
}

func (s *Store) Stat(ctx context.Context, logicalPath string) (guardianapi.FileInfo, error) {
	select {
	case <-ctx.Done():
		return guardianapi.FileInfo{}, ctx.Err()
	default:
	}
	logicalPath = normalizeLogicalPath(logicalPath)
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.files[logicalPath]
	if !ok || rec.current == nil || rec.current.version.Tombstone {
		return guardianapi.FileInfo{}, os.ErrNotExist
	}
	return guardianapi.FileInfo{
		Path:      logicalPath,
		Size:      int64(len(rec.current.content)),
		VersionID: rec.current.version.VersionID,
		ModTime:   rec.current.version.CommittedAt,
	}, nil
}

func (s *Store) ListDir(ctx context.Context, logicalDir string) ([]guardianapi.DirEntry, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	logicalDir = normalizeLogicalPath(logicalDir)
	prefix := logicalDir
	if prefix != "/" {
		prefix += "/"
	}

	entries := map[string]guardianapi.DirEntry{}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for logicalPath, rec := range s.files {
		if rec.current == nil || rec.current.version.Tombstone {
			continue
		}
		if logicalDir != "/" && !strings.HasPrefix(logicalPath, prefix) {
			continue
		}
		if logicalDir == "/" && !strings.HasPrefix(logicalPath, "/") {
			continue
		}
		remainder := strings.TrimPrefix(logicalPath, prefix)
		if logicalDir == "/" {
			remainder = strings.TrimPrefix(logicalPath, "/")
		}
		if remainder == "" {
			continue
		}
		parts := strings.Split(remainder, "/")
		name := parts[0]
		entry := guardianapi.DirEntry{Name: name}
		if len(parts) > 1 {
			entry.IsDir = true
		} else {
			entry.Size = int64(len(rec.current.content))
		}
		existing, ok := entries[name]
		if ok && existing.IsDir {
			continue
		}
		if entry.IsDir && ok {
			entry.Size = existing.Size
		}
		entries[name] = entry
	}
	out := make([]guardianapi.DirEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) ListDirPage(ctx context.Context, logicalDir string, opts guardianapi.DirListOptions) (guardianapi.DirListPage, error) {
	entries, err := s.ListDir(ctx, logicalDir)
	if err != nil {
		return guardianapi.DirListPage{}, err
	}
	return paginateEntries(entries, opts)
}

func paginateEntries(entries []guardianapi.DirEntry, opts guardianapi.DirListOptions) (guardianapi.DirListPage, error) {
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

func (s *Store) UpsertFiles(ctx context.Context, batch guardianapi.MutationBatch) (guardianapi.BatchRevision, error) {
	select {
	case <-ctx.Done():
		return guardianapi.BatchRevision{}, ctx.Err()
	default:
	}
	now := time.Now().UTC()
	batchRevisionID := revisions.NewBatchRevisionID()
	events := make([]guardianapi.ChangeEvent, 0, len(batch.Writes))

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, write := range batch.Writes {
		logicalPath := normalizeLogicalPath(write.LogicalPath)
		rec, ok := s.files[logicalPath]
		if !ok {
			rec = &fileRecord{}
			s.files[logicalPath] = rec
		}
		if err := checkExpectedVersion(rec.current, write.ExpectedVersionID); err != nil {
			return guardianapi.BatchRevision{}, fmt.Errorf("write %s: %w", logicalPath, err)
		}
	}

	result := guardianapi.BatchRevision{BatchRevisionID: batchRevisionID}
	for _, write := range batch.Writes {
		logicalPath := normalizeLogicalPath(write.LogicalPath)
		rec := s.files[logicalPath]
		version := guardianapi.FileVersion{
			LogicalPath:     logicalPath,
			VersionID:       revisions.NewVersionID(),
			BatchRevisionID: batchRevisionID,
			ContentSHA256:   digest.ContentHash(write.Content),
			CommittedAt:     now,
			PrincipalID:     batch.Context.PrincipalID,
			Reason:          batch.Context.Reason,
		}
		vr := versionRecord{version: version, content: copyBytes(write.Content)}
		rec.current = &vr
		rec.history = append(rec.history, vr)
		result.Files = append(result.Files, version)
		eventType := guardianapi.ChangeModified
		if len(rec.history) == 1 || (len(rec.history) > 1 && rec.history[len(rec.history)-2].version.Tombstone) {
			eventType = guardianapi.ChangeAdded
		}
		events = append(events, guardianapi.ChangeEvent{
			LogicalPath:     logicalPath,
			Type:            eventType,
			VersionID:       version.VersionID,
			BatchRevisionID: batchRevisionID,
			CommittedAt:     now,
		})
	}
	s.emitEventsLocked(ctx, events)
	return result, nil
}

func (s *Store) DeletePaths(ctx context.Context, batch guardianapi.DeleteBatch) (guardianapi.BatchRevision, error) {
	select {
	case <-ctx.Done():
		return guardianapi.BatchRevision{}, ctx.Err()
	default:
	}
	now := time.Now().UTC()
	batchRevisionID := revisions.NewBatchRevisionID()
	events := make([]guardianapi.ChangeEvent, 0, len(batch.Deletes))

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, del := range batch.Deletes {
		logicalPath := normalizeLogicalPath(del.LogicalPath)
		rec, ok := s.files[logicalPath]
		if !ok {
			rec = &fileRecord{}
			s.files[logicalPath] = rec
		}
		if err := checkExpectedVersion(rec.current, del.ExpectedVersionID); err != nil {
			return guardianapi.BatchRevision{}, fmt.Errorf("delete %s: %w", logicalPath, err)
		}
	}

	result := guardianapi.BatchRevision{BatchRevisionID: batchRevisionID}
	for _, del := range batch.Deletes {
		logicalPath := normalizeLogicalPath(del.LogicalPath)
		rec := s.files[logicalPath]
		version := guardianapi.FileVersion{
			LogicalPath:     logicalPath,
			VersionID:       revisions.NewVersionID(),
			BatchRevisionID: batchRevisionID,
			ContentSHA256:   digest.ContentHash(nil),
			CommittedAt:     now,
			Tombstone:       true,
			PrincipalID:     batch.Context.PrincipalID,
			Reason:          batch.Context.Reason,
		}
		vr := versionRecord{version: version}
		rec.current = &vr
		rec.history = append(rec.history, vr)
		result.Files = append(result.Files, version)
		events = append(events, guardianapi.ChangeEvent{
			LogicalPath:     logicalPath,
			Type:            guardianapi.ChangeDeleted,
			VersionID:       version.VersionID,
			BatchRevisionID: batchRevisionID,
			CommittedAt:     now,
		})
	}
	s.emitEventsLocked(ctx, events)
	return result, nil
}

func (s *Store) ListVersions(ctx context.Context, logicalPath string) ([]guardianapi.FileVersion, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	logicalPath = normalizeLogicalPath(logicalPath)
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.files[logicalPath]
	if !ok {
		return nil, nil
	}
	out := make([]guardianapi.FileVersion, 0, len(rec.history))
	for i := len(rec.history) - 1; i >= 0; i-- {
		out = append(out, rec.history[i].version)
	}
	return out, nil
}

func (s *Store) GetVersion(ctx context.Context, logicalPath, versionID string) (guardianapi.VersionedFile, error) {
	select {
	case <-ctx.Done():
		return guardianapi.VersionedFile{}, ctx.Err()
	default:
	}
	logicalPath = normalizeLogicalPath(logicalPath)
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.files[logicalPath]
	if !ok {
		return guardianapi.VersionedFile{}, os.ErrNotExist
	}
	for _, version := range rec.history {
		if version.version.VersionID == versionID {
			return guardianapi.VersionedFile{Version: version.version, Content: copyBytes(version.content)}, nil
		}
	}
	return guardianapi.VersionedFile{}, os.ErrNotExist
}

func (s *Store) Watch(ctx context.Context, prefixes []string) (<-chan guardianapi.ChangeEvent, error) {
	normalized := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		normalized = append(normalized, normalizeLogicalPath(prefix))
	}
	ch := make(chan guardianapi.ChangeEvent, 64)

	s.mu.Lock()
	id := s.nextWatch
	s.nextWatch++
	s.watchers[id] = watcherRegistration{prefixes: normalized, ch: ch}
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.watchers, id)
		close(ch)
		s.mu.Unlock()
	}()

	return ch, nil
}

func checkExpectedVersion(current *versionRecord, expected string) error {
	switch expected {
	case "":
		return nil
	case "absent":
		if current != nil && !current.version.Tombstone {
			return guardianapi.ErrConflict
		}
		return nil
	default:
		if current == nil || current.version.Tombstone || current.version.VersionID != expected {
			return guardianapi.ErrConflict
		}
		return nil
	}
}

func (s *Store) emitEventsLocked(ctx context.Context, events []guardianapi.ChangeEvent) {
	watchers := make([]watcherRegistration, 0, len(s.watchers))
	for _, watcher := range s.watchers {
		watchers = append(watchers, watcher)
	}
	for _, event := range events {
		for _, watcher := range watchers {
			if !matchesAnyPrefix(event.LogicalPath, watcher.prefixes) {
				continue
			}
			select {
			case watcher.ch <- event:
			case <-ctx.Done():
				return
			default:
			}
		}
	}
}

func matchesAnyPrefix(logicalPath string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix == "/" || logicalPath == prefix || strings.HasPrefix(logicalPath, strings.TrimSuffix(prefix, "/")+"/") {
			return true
		}
	}
	return false
}

var _ guardianapi.Store = (*Store)(nil)

var _ = errors.Is
