package historyquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

const DefaultDeploymentLimit = 10
const archiveScanPageSize = 256

type DeploymentFilter struct {
	Limit int
	Since *time.Time
	Until *time.Time
}

type DeploymentIndex struct {
	APIVersion string                           `json:"apiVersion"`
	Kind       string                           `json:"kind"`
	Partition  string                           `json:"partition"`
	Intent     string                           `json:"intent"`
	Records    []historydomain.DeploymentRecord `json:"records"`
}

func (f DeploymentFilter) Validate() error {
	if f.Limit < 0 {
		return fmt.Errorf("limit must be zero or greater")
	}
	if f.Since != nil && f.Until != nil && f.Since.After(*f.Until) {
		return fmt.Errorf("since must be before or equal to until")
	}
	return nil
}

func (f DeploymentFilter) Match(t time.Time) bool {
	if f.Since != nil && t.Before(*f.Since) {
		return false
	}
	if f.Until != nil && t.After(*f.Until) {
		return false
	}
	return true
}

func LoadDeploymentRecords(ctx context.Context, store guardianapi.ReadStore, partitionName, intentName string, filter DeploymentFilter) ([]historydomain.DeploymentRecord, error) {
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	records, err := loadDeploymentRecordsFromIndex(ctx, store, partitionName, intentName)
	if err == nil {
		return filterAndLimitRecords(records, filter), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	records, err = loadDeploymentRecordsFromArchiveScan(ctx, store, partitionName, intentName)
	if err != nil {
		return nil, err
	}
	return filterAndLimitRecords(records, filter), nil
}

func PrepareArchiveIndexContent(ctx context.Context, store guardianapi.ReadStore, record historydomain.DeploymentRecord) ([]byte, error) {
	records, err := loadDeploymentRecordsFromIndex(ctx, store, record.Partition, record.Intent)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		records, err = loadDeploymentRecordsFromArchiveScan(ctx, store, record.Partition, record.Intent)
		if err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	records = upsertRecord(records, record)
	index := DeploymentIndex{
		APIVersion: "guardian/v1alpha1",
		Kind:       "DeploymentIndex",
		Partition:  record.Partition,
		Intent:     record.Intent,
		Records:    records,
	}
	return json.MarshalIndent(index, "", "  ")
}

func loadDeploymentRecordsFromIndex(ctx context.Context, store guardianapi.ReadStore, partitionName, intentName string) ([]historydomain.DeploymentRecord, error) {
	content, err := store.ReadFile(ctx, paths.ArchiveIndex(partitionName, intentName))
	if err != nil {
		return nil, err
	}
	var index DeploymentIndex
	if err := json.Unmarshal(content, &index); err != nil {
		return nil, err
	}
	records := append([]historydomain.DeploymentRecord(nil), index.Records...)
	sortRecords(records)
	return records, nil
}

func loadDeploymentRecordsFromArchiveScan(ctx context.Context, store guardianapi.ReadStore, partitionName, intentName string) ([]historydomain.DeploymentRecord, error) {
	entries, err := listArchiveEntries(ctx, store, partitionName, intentName)
	if err != nil {
		return nil, err
	}
	records := make([]historydomain.DeploymentRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		record, err := loadDeploymentRecord(ctx, store, partitionName, intentName, entry.Name)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	sortRecords(records)
	return records, nil
}

func listArchiveEntries(ctx context.Context, store guardianapi.ReadStore, partitionName, intentName string) ([]guardianapi.DirEntry, error) {
	logicalDir := paths.ArchiveIntentRoot(partitionName, intentName)
	if paged, ok := store.(guardianapi.PagedDirLister); ok {
		return listArchiveEntriesPaged(ctx, paged, logicalDir)
	}
	entries, err := store.ListDir(ctx, logicalDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

func listArchiveEntriesPaged(ctx context.Context, store guardianapi.PagedDirLister, logicalDir string) ([]guardianapi.DirEntry, error) {
	out := make([]guardianapi.DirEntry, 0)
	offset := 0
	for {
		page, err := store.ListDirPage(ctx, logicalDir, guardianapi.DirListOptions{
			Offset: offset,
			Limit:  archiveScanPageSize,
		})
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
		out = append(out, page.Entries...)
		if !page.HasMore {
			return out, nil
		}
		offset = page.NextOffset
	}
}

func filterAndLimitRecords(records []historydomain.DeploymentRecord, filter DeploymentFilter) []historydomain.DeploymentRecord {
	filtered := make([]historydomain.DeploymentRecord, 0, len(records))
	for _, record := range records {
		if !filter.Match(record.CreatedAt) {
			continue
		}
		filtered = append(filtered, record)
	}
	if filter.Limit > 0 && len(filtered) > filter.Limit {
		return append([]historydomain.DeploymentRecord(nil), filtered[:filter.Limit]...)
	}
	return filtered
}

func sortRecords(records []historydomain.DeploymentRecord) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
}

func upsertRecord(records []historydomain.DeploymentRecord, record historydomain.DeploymentRecord) []historydomain.DeploymentRecord {
	out := make([]historydomain.DeploymentRecord, 0, len(records)+1)
	replaced := false
	for _, existing := range records {
		if existing.DeploymentRevision == record.DeploymentRevision {
			if !replaced {
				out = append(out, record)
				replaced = true
			}
			continue
		}
		out = append(out, existing)
	}
	if !replaced {
		out = append(out, record)
	}
	sortRecords(out)
	return out
}

func loadDeploymentRecord(ctx context.Context, store guardianapi.ReadStore, partitionName, intentName, deploymentRevision string) (historydomain.DeploymentRecord, error) {
	var record historydomain.DeploymentRecord
	content, err := store.ReadFile(ctx, paths.ArchiveState(partitionName, intentName, deploymentRevision))
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(content, &record); err != nil {
		return record, err
	}
	return record, nil
}
