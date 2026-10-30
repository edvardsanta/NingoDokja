package communications

import (
	"fmt"
	"strings"
)

// LinkAction is an application operation on a platform account. The transport
// still owns the platform session and the ingress still authenticates callers.
type LinkAction string

const (
	LinkStatus LinkAction = "communications.link.status"
	LinkStart  LinkAction = "communications.link.start"
	LinkReset  LinkAction = "communications.link.reset"
	// LinkGroups lists the groups the linked account takes part in, so the owner can choose which
	// to allow. It only reads.
	LinkGroups LinkAction = "communications.link.groups"
)

// LinkPolicy authorizes a named, configured platform for a supported operation.
// An adapter being present is necessary, but its presence does not grant an
// unknown action permission to mutate a session.
type LinkPolicy struct {
	Available map[string]bool
}

func (p LinkPolicy) Authorize(action LinkAction, platform string) (string, error) {
	platform = strings.TrimSpace(platform)
	if platform == "" || !p.Available[platform] {
		return "", fmt.Errorf("interface %q cannot be linked from here", platform)
	}
	switch action {
	case LinkStatus, LinkStart, LinkReset, LinkGroups:
		return platform, nil
	default:
		return "", fmt.Errorf("unsupported link action")
	}
}

// LinkedGroup is a group the linked account takes part in, as the owner chooses which to allow: its
// id, its name and how many take part. Listing a group allows nothing by itself.
type LinkedGroup struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Members int    `json:"members"`
}
