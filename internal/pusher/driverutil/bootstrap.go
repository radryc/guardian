package driverutil

import (
	"fmt"
	"strings"

	assetdefs "github.com/radryc/guardian/internal/domain/assets"
)

func FirstPort(ports []assetdefs.PortSpec) int {
	for _, port := range ports {
		if port.ServicePort != nil {
			return *port.ServicePort
		}
		if port.Port != nil {
			return *port.Port
		}
		if port.ContainerPort != nil {
			return *port.ContainerPort
		}
	}
	return 0
}

// BuildBootstrapEntry builds one segment of the LB_BOOTSTRAP string for a
// single ListenerSpec and its resolved backend addresses.
//
// Output format:
//
//	[name[@protocol][description]:]externalPort=backend1,backend2,...
//
// Port allocation:
//   - Static (externalPort set):  uses the pinned value.
//   - Static (default):           uses listener.Port as external port.
//   - Dynamic (dynamic: true):    omits externalPort so the lb edge allocates
//     a free port at runtime.

func BuildBootstrapEntry(listener assetdefs.ListenerSpec, backends []string) string {
	if listener.Port == nil {
		return ""
	}

	// --- LHS: [name[@protocol][description]:]port ---
	var lhs string
	hasLabel := listener.Name != "" || listener.Protocol != ""
	if hasLabel {
		name := listener.Name
		if name == "" {
			name = fmt.Sprintf("listener-%d", *listener.Port)
		}
		var proto string
		if listener.Protocol != "" {
			proto = "@" + strings.ToLower(listener.Protocol)
		}
		var desc string
		if listener.Description != "" {
			desc = "[" + listener.Description + "]"
		}
		// external port segment
		var extPort string
		if listener.Dynamic {
			// dynamic: no external port → lb allocates one
			extPort = ""
		} else if listener.ExternalPort != nil {
			extPort = fmt.Sprintf(":%d", *listener.ExternalPort)
		} else {
			extPort = fmt.Sprintf(":%d", *listener.Port)
		}
		lhs = name + proto + desc + extPort
	} else {
		if listener.Dynamic {
			lhs = ""
		} else if listener.ExternalPort != nil {
			lhs = fmt.Sprintf("%d", *listener.ExternalPort)
		} else {
			lhs = fmt.Sprintf("%d", *listener.Port)
		}
	}

	return lhs + "=" + strings.Join(backends, ",")
}

func sanitizePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}
