package runtime

import "strings"

func ResolvePrincipalID(pusherName, explicit string) string {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit
	}
	pusherName = strings.TrimSpace(pusherName)
	if pusherName == "" {
		return "guardian-pusher"
	}
	return "guardian-pusher-" + pusherName
}
