// Package access loads and compiles the repo-managed access policy
// (access.yaml) published into the store under /.access and exposes the
// decisions the UI needs: partition read visibility, admin checks, and the
// compiled bundle served to MonoFS and Doctor.
//
// Rollout is staged: with no RawPath the feature is disabled and every call is
// permissive; when enabled with Enforce=false it is observe-only (denials are
// logged but nothing is filtered); when Enforce=true reads are filtered and a
// policy that never loaded fails closed for non-admins.
package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	coreauthz "github.com/radryc/monofs/pkg/authz"
	"github.com/radryc/monofs/pkg/authz/policy"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// DefaultInterval is the raw policy/catalog refresh interval.
const DefaultInterval = 30 * time.Second

// Options configures a policy Service.
type Options struct {
	// Store provides read access to the published policy and the partition
	// catalog.
	Store guardianapi.ReadStore
	// RawPath is the logical store path of the raw ACL; empty disables the
	// feature entirely.
	RawPath string
	// Enforce turns filtering on. When false the service only observes.
	Enforce bool
	// Interval is the refresh period; <=0 falls back to DefaultInterval.
	Interval time.Duration
	// Logf receives refresh/observe messages; nil discards them.
	Logf func(string, ...any)
}

// Service keeps the latest successfully compiled policy and the catalog it was
// compiled against. It is safe for concurrent use.
type Service struct {
	store    guardianapi.ReadStore
	rawPath  string
	enforce  bool
	interval time.Duration
	logf     func(string, ...any)

	mu            sync.RWMutex
	policy        *policy.Policy
	rawDigest     string
	catalogDigest string
	lastErr       error
}

// NewService builds a policy service. A service with an empty RawPath is
// disabled and all decisions are permissive.
func NewService(opts Options) *Service {
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Service{
		store:    opts.Store,
		rawPath:  strings.TrimSpace(opts.RawPath),
		enforce:  opts.Enforce,
		interval: interval,
		logf:     logf,
	}
}

// Enabled reports whether a raw policy path is configured.
func (s *Service) Enabled() bool {
	return s != nil && s.rawPath != ""
}

// Ready reports whether a policy has been compiled and loaded successfully.
func (s *Service) Ready() bool {
	return s.current() != nil
}

// Version returns the loaded policy version ("" when not ready).
func (s *Service) Version() string {
	return s.current().Version()
}

// LastError returns the most recent load/compile error.
func (s *Service) LastError() error {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastErr
}

// PolicyJSON renders the compiled bundle for the /api/access/policy endpoint.
func (s *Service) PolicyJSON() ([]byte, error) {
	p := s.current()
	if p == nil {
		return nil, fmt.Errorf("access policy: no policy loaded")
	}
	return policy.MarshalPolicy(p)
}

// Run refreshes immediately and then on every interval until ctx is done. It
// is a no-op when the feature is disabled.
func (s *Service) Run(ctx context.Context) {
	if s == nil || !s.Enabled() {
		return
	}
	if err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
		s.logf("access policy: initial load from %s failed: %v", s.rawPath, err)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
				s.logf("access policy: refresh from %s failed: %v", s.rawPath, err)
			}
		}
	}
}

// Refresh reads the raw policy (resolving imports from the store), rebuilds the
// partition catalog, and recompiles. When neither the raw bytes nor the catalog
// changed since the last successful compile the work is skipped. On error the
// last good policy is kept and LastError is set.
func (s *Service) Refresh(ctx context.Context) error {
	if s == nil || !s.Enabled() {
		return nil
	}
	if s.store == nil {
		return s.fail(fmt.Errorf("access policy: no store configured"))
	}
	raw, err := s.store.ReadFile(ctx, s.rawPath)
	if err != nil {
		return s.fail(fmt.Errorf("access policy: read %s: %w", s.rawPath, err))
	}
	catalog, err := LoadCatalog(ctx, s.store)
	if err != nil {
		return s.fail(err)
	}
	rawDigest := digestBytes(raw)
	catalogDigest := catalogDigest(catalog)

	s.mu.RLock()
	unchanged := s.policy != nil && rawDigest == s.rawDigest && catalogDigest == s.catalogDigest
	s.mu.RUnlock()
	if unchanged {
		s.setLastError(nil)
		return nil
	}

	read := func(rel string) ([]byte, error) {
		return s.store.ReadFile(ctx, path.Join(path.Dir(s.rawPath), rel))
	}
	file, warnings, err := policy.LoadFile(raw, s.rawPath, read)
	if err != nil {
		return s.fail(err)
	}
	for _, warning := range warnings {
		s.logf("access policy: %s", warning)
	}
	bundle, issues, err := policy.Compile(file, catalog, time.Now())
	for _, issue := range issues {
		s.logf("access policy: %s: %s", issue.Severity, issue.Message)
	}
	if err != nil {
		return s.fail(err)
	}
	compiled := policy.New(*bundle)

	s.mu.Lock()
	oldVersion := ""
	if s.policy != nil {
		oldVersion = s.policy.Version()
	}
	s.policy = compiled
	s.rawDigest = rawDigest
	s.catalogDigest = catalogDigest
	s.lastErr = nil
	s.mu.Unlock()

	if oldVersion != compiled.Version() {
		s.logf("access policy: loaded version=%s path=%s enforce=%t", compiled.Version(), s.rawPath, s.enforce)
	}
	return nil
}

// CanSee reports whether the request identity may see the partition. When the
// feature is disabled everything is visible; when enabled without enforcement
// the decision is observed (logged) but not applied; when enforcement is on
// the check fails closed while no policy is loaded.
func (s *Service) CanSee(ctx context.Context, partition string) bool {
	if s == nil || !s.Enabled() {
		return true
	}
	p := s.current()
	if p == nil {
		return !s.enforce
	}
	id := identityFromContext(ctx)
	visible := p.CanSeePartition(id, partition)
	if !visible && !s.enforce {
		s.logf("access policy: observe mode: principal %s would be denied partition %q", principalLabel(id), partition)
	}
	return !s.enforce || visible
}

// IsAdmin reports whether the request identity holds the admin role. It is
// always false while no policy is loaded.
func (s *Service) IsAdmin(ctx context.Context) bool {
	p := s.current()
	if p == nil {
		return false
	}
	return p.IsAdmin(identityFromContext(ctx))
}

// CanModify reports whether the request identity may modify the partition
// (maintainer/admin role). It is permissive when the feature is disabled or in
// observe mode; the existing control-plane authz Guard still applies.
func (s *Service) CanModify(ctx context.Context, partition string) bool {
	if s == nil || !s.Enabled() || !s.enforce {
		return true
	}
	p := s.current()
	if p == nil {
		return false
	}
	id := identityFromContext(ctx)
	if p.IsAdmin(id) {
		return true
	}
	return p.Role(id, partition).Allows(coreauthz.ActionModify)
}

// Explain describes the grants matching a subject, mirroring the /api/access/
// explain endpoint. user is matched as an OIDC subject, or as an email when it
// contains "@".
func (s *Service) Explain(_ context.Context, user string, groups []string) policy.Explanation {
	user = strings.TrimSpace(user)
	id := coreauthz.Identity{Subject: user, Groups: append([]string(nil), groups...)}
	if strings.Contains(user, "@") {
		id.Email = user
	}
	return s.current().Explain(id)
}

// FilterPartitions keeps the partition names the request identity may see,
// preserving order. It is a no-op when the feature is disabled or observing.
func (s *Service) FilterPartitions(ctx context.Context, partitions []string) []string {
	if s == nil || !s.Enabled() {
		return partitions
	}
	out := make([]string, 0, len(partitions))
	for _, name := range partitions {
		if s.CanSee(ctx, name) {
			out = append(out, name)
		}
	}
	return out
}

func (s *Service) current() *policy.Policy {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policy
}

func (s *Service) fail(err error) error {
	s.setLastError(err)
	return err
}

func (s *Service) setLastError(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.lastErr = err
	s.mu.Unlock()
}

func identityFromContext(ctx context.Context) coreauthz.Identity {
	id, ok := coreauthz.IdentityFromContext(ctx)
	if !ok {
		return coreauthz.Identity{}
	}
	return id
}

func principalLabel(id coreauthz.Identity) string {
	switch {
	case id.ClientID != "":
		return "client:" + id.ClientID
	case id.Subject != "":
		return "user:" + id.Subject
	case id.Email != "":
		return "user:" + id.Email
	default:
		return "anonymous"
	}
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
