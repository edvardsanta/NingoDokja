package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// SettingGroups are the settings' groups, in the order the interfaces show them.
var SettingGroups = []string{"discord", "whatsapp", "models"}

// SettingName is a setting's id without its group, as the interfaces label it.
func SettingName(id string) string {
	_, name, _ := strings.Cut(id, ".")
	return name
}

// SettingValueText shows a value for people: a channel says whether the bot or a webhook
// delivers to it, an empty list what it means.
func SettingValueText(view SettingsView, row SettingRow) string {
	if row.Value == "" {
		switch {
		case row.ID == "discord.answer_channels":
			return "(every channel)"
		case row.Default != "":
			return row.Default + " (default)"
		case row.Kind == "text":
			return "(default)"
		}
		return "(none)"
	}
	if row.ID != "discord.meme_channels" {
		return strings.ReplaceAll(row.Value, ",", ", ")
	}
	var parts []string
	for _, id := range strings.Split(row.Value, ",") {
		path := "bot"
		if slices.Contains(view.WebhookChannels, id) {
			path = "webhook"
		}
		parts = append(parts, id+" ("+path+")")
	}
	return strings.Join(parts, ", ")
}

// SettingsImpact is what saving changed settings does: the modules recreated now and on their
// next start, and whether chat sessions end or stored items are embedded again.
type SettingsImpact struct {
	Now, Later []string
	Sessions   bool
	Reindex    bool
}

func ImpactOf(view SettingsView, ids []string, running map[string]bool) SettingsImpact {
	affects := map[string]bool{}
	var impact SettingsImpact
	for _, id := range ids {
		row, ok := view.Lookup(id)
		if !ok {
			continue
		}
		for _, name := range row.Affects {
			affects[name] = true
		}
		impact.Sessions = impact.Sessions || (row.Note == "sessions" && running["orchestrator"])
		impact.Reindex = impact.Reindex || row.Note == "reindex"
	}
	for name := range affects {
		if running[name] {
			impact.Now = append(impact.Now, name)
		} else {
			impact.Later = append(impact.Later, name)
		}
	}
	slices.Sort(impact.Now)
	slices.Sort(impact.Later)
	return impact
}

// SettingsChangeNotice says, in English, what saving the changed settings will do.
func SettingsChangeNotice(view SettingsView, ids []string, running map[string]bool) []string {
	impact := ImpactOf(view, ids, running)
	var lines []string
	if len(impact.Now) > 0 {
		lines = append(lines, "Recreated now: "+strings.Join(impact.Now, ", ")+".")
	}
	if len(impact.Later) > 0 {
		lines = append(lines, "Not running, recreated on their next start: "+strings.Join(impact.Later, ", ")+".")
	}
	if impact.Sessions {
		lines = append(lines, "Recreating the orchestrator ends the chat sessions in progress and briefly refuses every request.")
	}
	if impact.Reindex {
		lines = append(lines, "Memory and knowledge embed what they store again with the new model, a batch at a time.")
	}
	return lines
}

func (a *App) newSettingsCommand(ctx context.Context) *cobra.Command {
	command := &cobra.Command{
		Use:   "settings",
		Short: "Show or change the stack's settings (channels, people, models)",
		Long: "The settings are kept in the checkout's .env, through the local runtime controller. Credentials " +
			"are never settings: tokens and webhook addresses are edited in .env by hand.",
	}
	command.PersistentFlags().StringVar(&a.runtimeRoot, "runtime-root", "", "Checkout that holds docker-compose.lite.yml (default: found from the current directory)")

	list := &cobra.Command{
		Use:   "list",
		Short: "Show every setting by group",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.runtimeClient()
			if err != nil {
				return err
			}
			view, err := client.Settings(ctx)
			if err != nil {
				return err
			}
			printSettings(cmd.OutOrStdout(), view)
			return nil
		},
	}

	var yes bool
	set := &cobra.Command{
		Use:   "set <id>=<value> [<id>=<value>...]",
		Short: "Change settings and recreate the modules that read them",
		Example: "  dokja-cli settings set discord.direct_messages=closed\n" +
			"  dokja-cli settings set whatsapp.groups=123-456@g.us whatsapp.own_chat=true",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			values := map[string]string{}
			var ids []string
			for _, arg := range args {
				id, value, ok := strings.Cut(arg, "=")
				if !ok || id == "" {
					return fmt.Errorf("%q is not <id>=<value>", arg)
				}
				values[id] = value
				ids = append(ids, id)
			}
			client, err := a.runtimeClient()
			if err != nil {
				return err
			}
			view, err := client.Settings(ctx)
			if err != nil {
				return err
			}
			for _, id := range ids {
				if _, ok := view.Lookup(id); !ok {
					return fmt.Errorf("unknown setting %q (see `settings list`)", id)
				}
			}
			running := map[string]bool{}
			if snapshot, err := client.Request(ctx, "runtime.modules", nil); err == nil {
				for _, module := range snapshot.Modules {
					running[module.Name] = !module.Stopped()
				}
			}
			if !yes && !Confirm(a.stdin, cmd.OutOrStdout(), "Save?", SettingsChangeNotice(view, ids, running)) {
				return fmt.Errorf("nothing saved")
			}
			if _, err := client.SetSettings(ctx, values); err != nil {
				return err
			}
			view, err = waitForSettings(ctx, client)
			if err != nil {
				return err
			}
			if view.Error != "" {
				return fmt.Errorf("saved, but recreating a module failed: %s", view.Error)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Saved.")
			return nil
		},
	}
	set.Flags().BoolVar(&yes, "yes", false, "Save without asking")

	command.AddCommand(list, set)
	return command
}

func waitForSettings(ctx context.Context, client *RuntimeClient) (SettingsView, error) {
	for {
		view, err := client.Settings(ctx)
		if err != nil || !view.Busy {
			return view, err
		}
		select {
		case <-ctx.Done():
			return view, ctx.Err()
		case <-time.After(modulePoll):
		}
	}
}

func printSettings(out io.Writer, view SettingsView) {
	if view.Error != "" {
		fmt.Fprintln(out, "Last change: "+view.Error)
	}
	for _, group := range SettingGroups {
		fmt.Fprintln(out, group)
		for _, row := range view.Settings {
			if row.Group == group {
				fmt.Fprintf(out, "  %-20s %s\n", SettingName(row.ID), SettingValueText(view, row))
			}
		}
	}
	if len(view.WebhookChannels) > 0 {
		fmt.Fprintln(out, "Webhook channels are set in .env (DOKJA_DISCORD_WEBHOOKS) by hand; their addresses are credentials.")
	}
}
