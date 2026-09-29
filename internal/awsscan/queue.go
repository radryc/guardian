package awsscan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

const (
	defaultScanPollInterval = 5 * time.Second
	defaultScanTimeout      = 30 * time.Minute
	scanClaimLease          = 2 * time.Hour
)

type ScanRunner struct {
	PusherName           string
	Account              string
	Region               string
	AssumeRoleName       string
	AssumeRoleExternalID string
	WorkerID             string
	PrincipalID          string
	Store                guardianapi.Store
	PollInterval         time.Duration
	ScanTimeout          time.Duration
	// ShardIndex/ShardCount split scan requests across replicas by scan-ID hash.
	// ShardCount <= 1 disables sharding.
	ShardIndex int
	ShardCount int
	NewScanner func() Scanner
}

func (r *ScanRunner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	pollInterval := r.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultScanPollInterval
	}
	if err := r.ProcessPending(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("[ScanRunner] error on initial scan queue pass (will retry): %v", err)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.ProcessPending(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					return err
				}
				log.Printf("[ScanRunner] error processing scan queue (will retry in %s): %v", pollInterval, err)
			}
		}
	}
}

func (r *ScanRunner) validate() error {
	if r.Store == nil {
		return fmt.Errorf("scan runner store is required")
	}
	if strings.TrimSpace(r.PusherName) == "" {
		return fmt.Errorf("scan runner pusher name is required")
	}
	if r.NewScanner == nil {
		account := strings.TrimSpace(r.Account)
		assumeRole := strings.TrimSpace(r.AssumeRoleName)
		externalID := strings.TrimSpace(r.AssumeRoleExternalID)
		regionPin := strings.TrimSpace(r.Region)
		r.NewScanner = func() Scanner {
			return NewScanner(account, assumeRole, externalID, regionPin)
		}
	}
	if strings.TrimSpace(r.WorkerID) == "" {
		r.WorkerID = fmt.Sprintf("scan-worker-%d", time.Now().UnixNano())
	}
	return nil
}

func (r *ScanRunner) principal() string {
	if id := strings.TrimSpace(r.PrincipalID); id != "" {
		return id
	}
	return r.WorkerID
}

// ownsScan reports whether this replica is responsible for scanID when sharded.
func (r *ScanRunner) ownsScan(scanID string) bool {
	if r.ShardCount <= 1 {
		return true
	}
	shard := r.ShardIndex % r.ShardCount
	if shard < 0 {
		shard += r.ShardCount
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(scanID))
	return int(h.Sum32()%uint32(r.ShardCount)) == shard
}

func (r *ScanRunner) ProcessPending(ctx context.Context) error {
	if r.Store == nil {
		return fmt.Errorf("scan runner store is required")
	}
	pusher := strings.TrimSpace(r.PusherName)
	entries, err := r.Store.ListDir(ctx, paths.ScanRequestsDir(pusher))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") || strings.HasPrefix(entry.Name, ".") {
			continue
		}
		scanID := strings.TrimSuffix(entry.Name, ".json")
		if !r.ownsScan(scanID) {
			continue
		}
		if done, err := pathExists(ctx, r.Store, paths.ScanResult(pusher, scanID)); err != nil {
			return err
		} else if done {
			continue
		}
		if err := r.processScan(ctx, pusher, scanID); err != nil {
			log.Printf("[ScanRunner] scan %s failed: %v", scanID, err)
		}
	}
	return nil
}

func (r *ScanRunner) processScan(ctx context.Context, pusher, scanID string) error {
	ready, err := r.scanReadyToClaim(ctx, pusher, scanID)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}
	raw, err := r.Store.ReadFile(ctx, paths.ScanRequest(pusher, scanID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var request ScanRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return r.writeFailedResult(ctx, pusher, scanID, fmt.Errorf("decode scan request: %w", err))
	}
	if request.ScanID == "" {
		request.ScanID = scanID
	}
	if account := strings.TrimSpace(r.Account); account != "" && strings.TrimSpace(request.Account) != "" {
		if !strings.EqualFold(account, strings.TrimSpace(request.Account)) {
			return r.writeFailedResult(ctx, pusher, scanID, fmt.Errorf("scan request account %s does not match pusher account %s", request.Account, account))
		}
	}
	if account := strings.TrimSpace(request.Account); account == "" {
		request.Account = strings.TrimSpace(r.Account)
	}

	claimed, err := r.claimScan(ctx, pusher, scanID)
	if err != nil || !claimed {
		return err
	}

	log.Printf("[ScanRunner] claimed scan %s (account=%s regions=%v inventory=%t), executing...", scanID, request.Account, request.Regions, request.Inventory)

	scanner := r.NewScanner()
	scanCtx := ctx
	timeout := r.ScanTimeout
	if timeout <= 0 {
		timeout = defaultScanTimeout
	}
	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	result := scanner.Scan(scanCtx, &request)
	cancel()

	result.APIVersion = APIVersion
	result.Kind = ResultKind
	result.ScanID = scanID
	result.Pusher = pusher
	if strings.TrimSpace(result.Account) == "" {
		result.Account = request.Account
	}
	result.Request = &request
	if result.Status == "" {
		result.Status = ScanStatusFailed
		result.Errors = append(result.Errors, ScanError{Message: "scanner returned no status"})
	}
	fillSummary(result)

	if err := r.writeResult(ctx, pusher, result); err != nil {
		return fmt.Errorf("write scan result: %w", err)
	}
	r.releaseClaim(ctx, pusher, scanID)
	log.Printf("[ScanRunner] scan %s finished: status=%s buckets=%d fileSystems=%d parameters=%d secrets=%d services=%d loadBalancers=%d stacks=%d inventory=%d errors=%d",
		scanID, result.Status, result.Summary.BucketCount, result.Summary.FileSystemCount, result.Summary.ParameterCount,
		result.Summary.SecretCount, result.Summary.ServiceCount, result.Summary.LoadBalancerCount,
		result.Summary.StackCount, result.Summary.InventoryCount, result.Summary.ErrorCount)
	return nil
}

func (r *ScanRunner) scanReadyToClaim(ctx context.Context, pusher, scanID string) (bool, error) {
	claimPath := paths.ScanClaim(pusher, scanID)
	info, err := r.Store.Stat(ctx, claimPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	raw, err := r.Store.ReadFile(ctx, claimPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	var claim ScanClaim
	if err := json.Unmarshal(raw, &claim); err != nil {
		return false, fmt.Errorf("decode scan claim %s: %w", scanID, err)
	}
	if claim.ClaimedAt.IsZero() || claim.LeaseSeconds <= 0 {
		return true, nil
	}
	if time.Now().UTC().Before(claim.ClaimedAt.Add(time.Duration(claim.LeaseSeconds) * time.Second)) {
		return false, nil
	}
	_, err = r.Store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{
			LogicalPath:       claimPath,
			ExpectedVersionID: info.VersionID,
		}},
		Context: guardianapi.MutationContext{
			PrincipalID:   r.principal(),
			Reason:        "release expired scan claim",
			CorrelationID: scanID,
		},
	})
	if err != nil {
		if errors.Is(err, guardianapi.ErrConflict) || errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *ScanRunner) claimScan(ctx context.Context, pusher, scanID string) (bool, error) {
	claim := ScanClaim{
		ScanID:       scanID,
		WorkerID:     r.WorkerID,
		ClaimedAt:    time.Now().UTC(),
		LeaseSeconds: int(scanClaimLease.Seconds()),
	}
	content, err := json.MarshalIndent(claim, "", "  ")
	if err != nil {
		return false, err
	}
	_, err = r.Store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath:       paths.ScanClaim(pusher, scanID),
			Content:           content,
			ExpectedVersionID: "absent",
		}},
		Context: guardianapi.MutationContext{
			PrincipalID:   r.principal(),
			Reason:        "claim scan",
			CorrelationID: scanID,
		},
	})
	if err != nil {
		if errors.Is(err, guardianapi.ErrConflict) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *ScanRunner) releaseClaim(ctx context.Context, pusher, scanID string) {
	claimPath := paths.ScanClaim(pusher, scanID)
	info, err := r.Store.Stat(ctx, claimPath)
	if err != nil {
		return
	}
	_, _ = r.Store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{
			LogicalPath:       claimPath,
			ExpectedVersionID: info.VersionID,
		}},
		Context: guardianapi.MutationContext{
			PrincipalID:   r.principal(),
			Reason:        "release scan claim",
			CorrelationID: scanID,
		},
	})
}

func (r *ScanRunner) writeResult(ctx context.Context, pusher string, result *ScanResult) error {
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	_, err = r.Store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: paths.ScanResult(pusher, result.ScanID),
			Content:     content,
		}},
		Context: guardianapi.MutationContext{
			PrincipalID:   r.principal(),
			Reason:        "write scan result",
			CorrelationID: result.ScanID,
		},
	})
	return err
}

func (r *ScanRunner) writeFailedResult(ctx context.Context, pusher, scanID string, cause error) error {
	result := &ScanResult{
		APIVersion: APIVersion,
		Kind:       ResultKind,
		ScanID:     scanID,
		Pusher:     pusher,
		Account:    strings.TrimSpace(r.Account),
		Status:     ScanStatusFailed,
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
		Errors:     []ScanError{{Message: cause.Error()}},
	}
	return r.writeResult(ctx, pusher, result)
}

func pathExists(ctx context.Context, store guardianapi.ReadStore, logicalPath string) (bool, error) {
	_, err := store.Stat(ctx, logicalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
