package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// DeliveryModules answer or post outside once they run. Starting one is confirmed after saying what
// it will do with the switches and jobs already saved; nothing is changed on the way.
var DeliveryModules = map[string]bool{"discord": true, "scheduler": true}

// DeliveryNotice says what starting a delivery module will do, read from a ningo.status result.
// statusErr is the reason the status could not be read, if any.
func DeliveryNotice(module string, status any, statusErr error) []string {
	if statusErr != nil {
		return []string{fmt.Sprintf("Could not read the orchestrator (%v): starting %s acts on the switches and jobs already saved.", statusErr, module)}
	}
	switches := map[string]bool{}
	if services, err := domainField(status, "services"); err == nil {
		if rows, ok := services.(map[string]any); ok {
			for name, raw := range rows {
				if row, ok := raw.(map[string]any); ok {
					enabled, known := row["enabled"].(bool)
					switches[name] = !known || enabled
				}
			}
		}
	}
	on := func(name string) bool {
		enabled, known := switches[name]
		return !known || enabled
	}
	switch module {
	case "discord":
		lines := []string{"Voice starts as well; Discord needs it."}
		if on("discord") {
			return append(lines, "The bot goes online, answers mentions and commands in Discord and receives Ningo's deliveries.")
		}
		return append(lines, "The discord switch is off: the bot goes online but neither answers nor receives deliveries.")
	case "scheduler":
		if !on("scheduler") {
			return []string{"The scheduler switch is off: no scheduled job runs until it is switched on."}
		}
		var lines []string
		if jobs, err := domainField(status, "jobs"); err == nil {
			rows, _ := jobs.([]any)
			for _, raw := range rows {
				job, ok := raw.(map[string]any)
				if !ok || job["enabled"] != true {
					continue
				}
				name, _ := job["name"].(string)
				line := fmt.Sprintf("%s runs every %v", name, job["interval"])
				if next, _ := job["next_at"].(string); next != "" {
					line += ", next at " + next
				}
				lines = append(lines, line+": "+jobEffect(name, status)+".")
			}
		}
		if len(lines) == 0 {
			return []string{"Every scheduled job is paused: nothing runs until one is resumed."}
		}
		return lines
	}
	return nil
}

func jobEffect(name string, status any) string {
	switch name {
	case "meme.dispatch":
		count := 0
		if channels, err := domainField(status, "channels"); err == nil {
			if rows, ok := channels.(map[string]any); ok {
				meme, _ := rows["meme"].([]any)
				count = len(meme)
			}
		}
		return fmt.Sprintf("posts memes to %d Discord channel(s)", count)
	case "meme.refresh":
		return "refreshes the meme pool and posts nothing"
	}
	return "runs its scheduled event"
}

// Confirm prints the notice and reads a yes. Anything else, end of input included, is a no.
func Confirm(in io.Reader, out io.Writer, question string, lines []string) bool {
	for _, line := range lines {
		fmt.Fprintln(out, "  "+line)
	}
	fmt.Fprint(out, question+" [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "s", "sim":
		return true
	}
	return false
}
