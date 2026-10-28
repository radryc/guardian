package probes

import (
	"strings"

	"github.com/radryc/guardian/internal/onboarding"
	"github.com/radryc/guardian/internal/pusher/driverutil"
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
			url := strings.TrimSpace(probe.URL)
			address := strings.TrimSpace(probe.Address)
			// A construct naturally references an ALB DNS name without a
			// scheme. Treat that as an address so the HTTP probe can build a
			// valid http:// URL.
			if url != "" && !strings.Contains(url, "://") {
				address = url
				url = ""
			}
			probeType := strings.ToLower(strings.TrimSpace(probe.Type))
			if probeType == "" {
				switch {
				case url != "":
					probeType = inferProbeType(url, probe.Port)
				case address != "" && strings.TrimSpace(probe.Path) != "":
					probeType = "http"
				default:
					probeType = inferProbeType(address, probe.Port)
				}
			}
			port := probe.Port
			if port == 0 {
				switch probeType {
				case "http":
					port = 80
				case "https":
					port = 443
				}
			}
			target := Target{
				Name:         name,
				Partition:    descriptor.Partition,
				Intent:       descriptor.Intent,
				Asset:        asset,
				Mode:         "explicit",
				Type:         probeType,
				URL:          url,
				Address:      address,
				Port:         port,
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
