package probes

import (
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/onboarding"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
)

// TargetsFromOnboarding converts storeless onboarding descriptors into probe
// targets. The pusher uses this in --observed-only mode, where there is no
// MonoFS partition tree to discover targets from.
func TargetsFromOnboarding(pusher string, descriptors []onboarding.Descriptor) []Target {
	targets := make([]Target, 0)
	for _, descriptor := range descriptors {
		if strings.TrimSpace(descriptor.Partition) == "" {
			continue
		}
		asset := strings.TrimSpace(descriptor.Stack)
		if asset == "" {
			asset = strings.TrimSpace(descriptor.Intent)
		}
		for _, probe := range descriptor.Probes {
			name := strings.TrimSpace(probe.Name)
			if name == "" {
				name = "probe"
			}
			if strings.TrimSpace(probe.URL) == "" && strings.TrimSpace(probe.Address) == "" && strings.TrimSpace(probe.Path) == "" {
				continue
			}
			probeType := strings.ToLower(strings.TrimSpace(probe.Type))
			if probeType == "" {
				probeType = inferProbeType(probe.URL, probe.Port)
				if strings.TrimSpace(probe.Address) != "" {
					probeType = inferProbeType(probe.Address, probe.Port)
				}
			}
			target := Target{
				Name:         name,
				Partition:    descriptor.Partition,
				Intent:       descriptor.Intent,
				Asset:        asset,
				Mode:         "explicit",
				Type:         probeType,
				URL:          strings.TrimSpace(probe.URL),
				Address:      strings.TrimSpace(probe.Address),
				Port:         probe.Port,
				Path:         strings.TrimSpace(probe.Path),
				Method:       strings.TrimSpace(probe.Method),
				Headers:      probe.Headers,
				ExpectStatus: append([]int(nil), probe.ExpectStatus...),
				Interval:     parseDuration(probe.Interval, 0),
				Timeout:      parseDuration(probe.Timeout, 0),
				Labels:       probe.Labels,
			}
			if probe.Retries != nil {
				target.Retries = *probe.Retries
			}
			if probe.Insecure != nil {
				target.Insecure = *probe.Insecure
			}
			identity := driverutil.CanonicalServiceIdentity(descriptor.Partition, descriptor.Intent, asset)
			target.Service = identity.Name
			target.FullName = identity.FullName
			target.ID = ProbeID(pusher, target.Partition, target.Intent, target.Asset, target.Name, target.Type, targetValue(target))
			targets = append(targets, target)
		}
	}
	return targets
}
