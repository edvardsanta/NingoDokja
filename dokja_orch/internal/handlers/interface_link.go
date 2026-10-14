package handlers

import (
	"context"
	"fmt"
	"read_books/internal/core"
	"read_books/internal/logger"
	"strings"
)

// InterfaceLinker is an interface that links to an account (WhatsApp). Status gives its state and,
// while it waits for a link, the rows of the code to scan; Start asks for a new attempt; Reset
// unlinks the account and erases its saved session.
type InterfaceLinker interface {
	Status(ctx context.Context) (state string, matrix []string, err error)
	Start(ctx context.Context) (state string, err error)
	Reset(ctx context.Context) (state string, err error)
}

// WithInterfaceLinks names the interfaces that can be linked from the desktop. Only the ones
// given here answer the communications.link actions.
func (h *SystemDomainHandler) WithInterfaceLinks(links map[string]InterfaceLinker) *SystemDomainHandler {
	h.links = links
	return h
}

// handleInterfaceLink serves the three communications.link actions. They sit under the
// communications prefix, so the orchestrator has already checked the communications credential;
// the code is returned to the caller and is never logged.
func (h *SystemDomainHandler) handleInterfaceLink(ctx context.Context, event core.Event) (map[string]any, error) {
	name := strings.TrimSpace(resultString(event.Payload, "interface"))
	linker := h.links[name]
	if name == "" || linker == nil {
		return nil, fmt.Errorf("interface %q cannot be linked from here", name)
	}

	var (
		state  string
		matrix []string
		err    error
	)
	switch event.Type {
	case "communications.link.status":
		state, matrix, err = linker.Status(ctx)
	case "communications.link.start":
		state, err = linker.Start(ctx)
	case "communications.link.reset":
		state, err = linker.Reset(ctx)
	default:
		return nil, fmt.Errorf("unsupported link action")
	}
	if err != nil {
		// The error text is not returned: it could carry an address of the interface.
		return nil, fmt.Errorf("the %s interface did not answer the link request", name)
	}

	logger.Info(fmt.Sprintf("system handler interface link event_id=%s type=%s interface=%s state=%s", event.EventID, event.Type, name, state))
	reply := map[string]any{"interface": name, "state": state}
	if len(matrix) > 0 {
		reply["matrix"] = matrix
	}
	return reply, nil
}
