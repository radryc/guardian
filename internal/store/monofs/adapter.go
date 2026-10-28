package monofs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/radryc/guardian/pkg/guardianapi"
)

type MonoFSClient interface {
	UpsertPaths(ctx context.Context, token string, writes []guardianapi.PathWrite, mutationCtx guardianapi.MutationContext) (guardianapi.BatchRevision, error)
	DeletePaths(ctx context.Context, token string, deletes []guardianapi.PathDelete, mutationCtx guardianapi.MutationContext) (guardianapi.BatchRevision, error)
	ListVersions(ctx context.Context, token string, logicalPath string) ([]guardianapi.FileVersion, error)
	GetVersion(ctx context.Context, token string, logicalPath, versionID string) (guardianapi.VersionedFile, error)
	ReadFile(ctx context.Context, mountPath string) ([]byte, error)
	ListDir(ctx context.Context, mountPath string) ([]guardianapi.DirEntry, error)
}

type pagedMonoFSClient interface {
	ListDirPage(ctx context.Context, mountPath string, opts guardianapi.DirListOptions) (guardianapi.DirListPage, error)
}

type statMonoFSClient interface {
	StatAttr(ctx context.Context, mountPath string) (int64, time.Time, error)
	LatestVersion(ctx context.Context, token, logicalPath string) (guardianapi.FileVersion, bool, error)
}

type watcherClient interface {
	Watch(ctx context.Context, prefixes []string) (<-chan guardianapi.ChangeEvent, error)
}

type Adapter struct {
	client MonoFSClient
	token  string
}

func New(client MonoFSClient, token string) *Adapter {
	return &Adapter{client: client, token: token}
}

func (a *Adapter) ReadFile(ctx context.Context, logicalPath string) ([]byte, error) {
	mapped, err := mapLogicalToPhysicalChecked(logicalPath)
	if err != nil {
		return nil, err
	}
	return a.client.ReadFile(ctx, mapped)
}

func (a *Adapter) ListDir(ctx context.Context, logicalDir string) ([]guardianapi.DirEntry, error) {
	mapped, err := mapLogicalToPhysicalChecked(logicalDir)
	if err != nil {
		return nil, err
	}
	return a.client.ListDir(ctx, mapped)
}

func (a *Adapter) ListDirPage(ctx context.Context, logicalDir string, opts guardianapi.DirListOptions) (guardianapi.DirListPage, error) {
	mapped, err := mapLogicalToPhysicalChecked(logicalDir)
	if err != nil {
		return guardianapi.DirListPage{}, err
	}
	if client, ok := a.client.(pagedMonoFSClient); ok {
		return client.ListDirPage(ctx, mapped, opts)
	}
	entries, err := a.client.ListDir(ctx, mapped)
	if err != nil {
		return guardianapi.DirListPage{}, err
	}
	return paginateEntries(entries, opts)
}

func (a *Adapter) Stat(ctx context.Context, logicalPath string) (guardianapi.FileInfo, error) {
	if sc, ok := a.client.(statMonoFSClient); ok {
		mapped, err := mapLogicalToPhysicalChecked(logicalPath)
		if err != nil {
			return guardianapi.FileInfo{}, err
		}
		size, attrModTime, err := sc.StatAttr(ctx, mapped)
		if err != nil {
			return guardianapi.FileInfo{}, err
		}
		ver, found, err := sc.LatestVersion(ctx, a.token, logicalPath)
		if err != nil {
			return guardianapi.FileInfo{}, err
		}
		info := guardianapi.FileInfo{Path: logicalPath, Size: size}
		if found {
			info.VersionID = ver.VersionID
			info.ModTime = ver.CommittedAt
		} else if !attrModTime.IsZero() {
			info.ModTime = attrModTime
		} else {
			info.ModTime = time.Now().UTC()
		}
		return info, nil
	}

	content, err := a.ReadFile(ctx, logicalPath)
	if err != nil {
		return guardianapi.FileInfo{}, err
	}
	versions, err := a.ListVersions(ctx, logicalPath)
	if err != nil {
		return guardianapi.FileInfo{}, err
	}
	info := guardianapi.FileInfo{Path: logicalPath, Size: int64(len(content))}
	if len(versions) > 0 {
		info.VersionID = versions[0].VersionID
		info.ModTime = versions[0].CommittedAt
	} else {
		info.ModTime = time.Now().UTC()
	}
	return info, nil
}

// ReadFileWithInfo reads a file's content and fetches its latest version metadata
// in a single pass without downloading the file a second time for Stat().
func (a *Adapter) ReadFileWithInfo(ctx context.Context, logicalPath string) ([]byte, guardianapi.FileInfo, error) {
	content, err := a.ReadFile(ctx, logicalPath)
	if err != nil {
		return nil, guardianapi.FileInfo{}, err
	}
	info := guardianapi.FileInfo{
		Path: logicalPath,
		Size: int64(len(content)),
	}
	if sc, ok := a.client.(statMonoFSClient); ok {
		ver, found, verErr := sc.LatestVersion(ctx, a.token, logicalPath)
		if verErr != nil {
			return nil, guardianapi.FileInfo{}, verErr
		}
		if found {
			info.VersionID = ver.VersionID
			info.ModTime = ver.CommittedAt
		} else {
			info.ModTime = time.Now().UTC()
		}
		return content, info, nil
	}
	versions, err := a.ListVersions(ctx, logicalPath)
	if err != nil {
		return nil, guardianapi.FileInfo{}, err
	}
	if len(versions) > 0 {
		info.VersionID = versions[0].VersionID
		info.ModTime = versions[0].CommittedAt
	} else {
		info.ModTime = time.Now().UTC()
	}
	return content, info, nil
}

func (a *Adapter) Watch(ctx context.Context, prefixes []string) (<-chan guardianapi.ChangeEvent, error) {
	wc, ok := a.client.(watcherClient)
	if !ok {
		return nil, fmt.Errorf("monofs watch not supported by client")
	}
	ch, err := wc.Watch(ctx, prefixes)
	if err != nil {
		return nil, err
	}
	out := make(chan guardianapi.ChangeEvent)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				if logical, err := mapPhysicalToLogicalChecked(event.LogicalPath); err == nil {
					event.LogicalPath = logical
				}
				out <- event
			}
		}
	}()
	return out, nil
}

func (a *Adapter) UpsertFiles(ctx context.Context, batch guardianapi.MutationBatch) (guardianapi.BatchRevision, error) {
	result, err := a.client.UpsertPaths(ctx, a.token, batch.Writes, batch.Context)
	if err != nil {
		return guardianapi.BatchRevision{}, err
	}
	remapBatchRevision(&result)
	return result, nil
}

func (a *Adapter) DeletePaths(ctx context.Context, batch guardianapi.DeleteBatch) (guardianapi.BatchRevision, error) {
	result, err := a.client.DeletePaths(ctx, a.token, batch.Deletes, batch.Context)
	if err != nil {
		return guardianapi.BatchRevision{}, err
	}
	remapBatchRevision(&result)
	return result, nil
}

func (a *Adapter) ListVersions(ctx context.Context, logicalPath string) ([]guardianapi.FileVersion, error) {
	versions, err := a.client.ListVersions(ctx, a.token, logicalPath)
	if err != nil {
		return nil, err
	}
	for i := range versions {
		if mapped, err := mapPhysicalToLogicalChecked(versions[i].LogicalPath); err == nil {
			versions[i].LogicalPath = mapped
		}
	}
	return versions, nil
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

func (a *Adapter) GetVersion(ctx context.Context, logicalPath, versionID string) (guardianapi.VersionedFile, error) {
	file, err := a.client.GetVersion(ctx, a.token, logicalPath, versionID)
	if err != nil {
		return guardianapi.VersionedFile{}, err
	}
	if mapped, err := mapPhysicalToLogicalChecked(file.Version.LogicalPath); err == nil {
		file.Version.LogicalPath = mapped
	}
	return file, nil
}

func remapBatchRevision(result *guardianapi.BatchRevision) {
	for i := range result.Files {
		logical, err := mapPhysicalToLogicalChecked(result.Files[i].LogicalPath)
		if err == nil {
			result.Files[i].LogicalPath = logical
		}
	}
}

func mapLogicalToPhysicalChecked(logicalPath string) (string, error) {
	if logicalPath == "/partitions" {
		return "guardian", nil
	}
	if strings.HasPrefix(logicalPath, "/partitions/") {
		return "guardian/" + strings.TrimPrefix(logicalPath, "/partitions/"), nil
	}
	if logicalPath == "/.queues" {
		return "guardian-system/.queues", nil
	}
	if strings.HasPrefix(logicalPath, "/.queues/") {
		return "guardian-system/.queues/" + strings.TrimPrefix(logicalPath, "/.queues/"), nil
	}
	if logicalPath == "/.archive" {
		return "guardian-system/.archive", nil
	}
	if strings.HasPrefix(logicalPath, "/.archive/") {
		return "guardian-system/.archive/" + strings.TrimPrefix(logicalPath, "/.archive/"), nil
	}
	if logicalPath == "/.scans" {
		return "guardian-system/.scans", nil
	}
	if strings.HasPrefix(logicalPath, "/.scans/") {
		return "guardian-system/.scans/" + strings.TrimPrefix(logicalPath, "/.scans/"), nil
	}
	if logicalPath == "/.registry" {
		return "guardian-system/.registry", nil
	}
	if strings.HasPrefix(logicalPath, "/.registry/") {
		return "guardian-system/.registry/" + strings.TrimPrefix(logicalPath, "/.registry/"), nil
	}
	if logicalPath == "/.access" {
		return "guardian-system/.access", nil
	}
	if strings.HasPrefix(logicalPath, "/.access/") {
		return "guardian-system/.access/" + strings.TrimPrefix(logicalPath, "/.access/"), nil
	}
	// Global template overlays (/.templates/aws/**) let operators extend or
	// override the embedded CDK template catalog from MonoFS.
	if logicalPath == "/.templates" {
		return "guardian-system/.templates", nil
	}
	if strings.HasPrefix(logicalPath, "/.templates/") {
		return "guardian-system/.templates/" + strings.TrimPrefix(logicalPath, "/.templates/"), nil
	}
	// Repo display paths (ingested repositories) are reachable read-only under
	// /.repos/<display-path>/... so the repo importer can read
	// guardian/partitions/** straight from MonoFS.
	if strings.HasPrefix(logicalPath, "/.repos/") {
		trimmed := strings.TrimPrefix(logicalPath, "/.repos/")
		if trimmed == "" || strings.Contains(trimmed, "..") {
			return "", fmt.Errorf("unsupported guardian logical path: %s", logicalPath)
		}
		return trimmed, nil
	}
	return "", fmt.Errorf("unsupported guardian logical path: %s", logicalPath)
}

func mapPhysicalToLogicalChecked(physicalPath string) (string, error) {
	if physicalPath == "guardian" {
		return "/partitions", nil
	}
	if strings.HasPrefix(physicalPath, "guardian/") {
		return "/partitions/" + strings.TrimPrefix(physicalPath, "guardian/"), nil
	}
	if physicalPath == "guardian-system/.queues" {
		return "/.queues", nil
	}
	if strings.HasPrefix(physicalPath, "guardian-system/.queues/") {
		return "/.queues/" + strings.TrimPrefix(physicalPath, "guardian-system/.queues/"), nil
	}
	if physicalPath == "guardian-system/.archive" {
		return "/.archive", nil
	}
	if strings.HasPrefix(physicalPath, "guardian-system/.archive/") {
		return "/.archive/" + strings.TrimPrefix(physicalPath, "guardian-system/.archive/"), nil
	}
	if physicalPath == "guardian-system/.scans" {
		return "/.scans", nil
	}
	if strings.HasPrefix(physicalPath, "guardian-system/.scans/") {
		return "/.scans/" + strings.TrimPrefix(physicalPath, "guardian-system/.scans/"), nil
	}
	if physicalPath == "guardian-system/.registry" {
		return "/.registry", nil
	}
	if strings.HasPrefix(physicalPath, "guardian-system/.registry/") {
		return "/.registry/" + strings.TrimPrefix(physicalPath, "guardian-system/.registry/"), nil
	}
	if physicalPath == "guardian-system/.access" {
		return "/.access", nil
	}
	if strings.HasPrefix(physicalPath, "guardian-system/.access/") {
		return "/.access/" + strings.TrimPrefix(physicalPath, "guardian-system/.access/"), nil
	}
	if physicalPath == "guardian-system/.templates" {
		return "/.templates", nil
	}
	if strings.HasPrefix(physicalPath, "guardian-system/.templates/") {
		return "/.templates/" + strings.TrimPrefix(physicalPath, "guardian-system/.templates/"), nil
	}
	return "", fmt.Errorf("unsupported monofs physical path: %s", physicalPath)
}

var _ guardianapi.Store = (*Adapter)(nil)
