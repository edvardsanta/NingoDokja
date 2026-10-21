package cli

import (
	"errors"
	"strings"
)

// LinkableInterfaces are the interfaces the orchestrator can link to an account.
var LinkableInterfaces = []string{"whatsapp"}

// CommunicationsContext adds the credential communications.* actions need, from
// DOKJA_COMMUNICATIONS_TOKEN (the value the orchestrator and the desktop shell share). The
// orchestrator strips it before anything else sees the event; it is never printed here.
func CommunicationsContext(eventType string, context map[string]any, getenv func(string) string) map[string]any {
	if !strings.HasPrefix(eventType, "communications.") {
		return context
	}
	token := strings.TrimSpace(getenv("DOKJA_COMMUNICATIONS_TOKEN"))
	if token == "" {
		return context
	}
	out := make(map[string]any, len(context)+1)
	for key, value := range context {
		out[key] = value
	}
	out["communications_token"] = token
	return out
}

// LinkView is what the orchestrator says about linking an interface.
type LinkView struct {
	Interface string
	State     string
	// Matrix holds the rows of the code to scan, while the interface waits for one.
	Matrix []string
}

func ParseLink(result map[string]any) LinkView {
	view := LinkView{}
	view.Interface, _ = result["interface"].(string)
	view.State, _ = result["state"].(string)
	if rows, ok := result["matrix"].([]any); ok {
		for _, row := range rows {
			if text, ok := row.(string); ok {
				view.Matrix = append(view.Matrix, text)
			}
		}
	}
	return view
}

const (
	minCodeSide = 21
	maxCodeSide = 177
	quietZone   = 4
)

// RenderLinkCode draws the code for a terminal, two rows of modules per line with half blocks,
// in explicit black and white so it scans on any terminal theme.
func RenderLinkCode(rows []string) (string, error) {
	side := len(rows)
	if side < minCodeSide || side > maxCodeSide {
		return "", errors.New("the link code has an unexpected size")
	}
	for _, row := range rows {
		if len(row) != side || strings.Trim(row, "01") != "" {
			return "", errors.New("the link code is malformed")
		}
	}
	size := side + 2*quietZone
	dark := func(x, y int) bool {
		x, y = x-quietZone, y-quietZone
		return x >= 0 && y >= 0 && x < side && y < side && rows[y][x] == '1'
	}
	var b strings.Builder
	for y := 0; y < size; y += 2 {
		b.WriteString("\x1b[97;40m")
		for x := 0; x < size; x++ {
			top, bottom := !dark(x, y), y+1 < size && !dark(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), nil
}

// LinkStateText says what a link state means, in English.
func LinkStateText(state string) string {
	switch state {
	case "connected":
		return "linked"
	case "waiting_for_link":
		return "waiting for the code to be scanned"
	case "connecting":
		return "connecting"
	case "logged_out":
		return "unlinked"
	case "refused":
		return "the last code was refused; ask for a new one"
	case "replaced":
		return "the session was opened elsewhere"
	case "":
		return "unknown"
	}
	return state
}

func checkLinkable(name string) error {
	return checkName("interface", name, LinkableInterfaces)
}
