package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type ProviderRegistration struct {
	ID      string   `json:"id"`
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models"`
}
type ProviderSaved struct {
	Revision uint64 `json:"configuration_revision"`
	Provider string `json:"provider"`
}

func (c *RuntimeClient) ConfigureProvider(ctx context.Context, revision uint64, p ProviderRegistration) (ProviderSaved, error) {
	if p.Models == nil {
		p.Models = []string{}
	}
	var result ProviderSaved
	err := c.call(ctx, "runtime.providers.configure", map[string]any{"configuration_revision": revision, "provider": p.ID, "enabled": p.Enabled, "models": p.Models}, &result)
	if err != nil {
		return result, err
	}
	if result.Revision != revision+1 || result.Provider != p.ID {
		return ProviderSaved{}, fmt.Errorf("incompatible provider save response; reload configuration")
	}
	return result, nil
}

// DeclaredProviderModels accepts a comma-separated operator list, not provider output.
func DeclaredProviderModels(text string) []string {
	names := []string{}
	for _, raw := range strings.Split(text, ",") {
		if name := strings.TrimSpace(raw); name != "" {
			names = append(names, name)
		}
	}
	return names
}
func (a *App) newProviderConfigurationCommand(ctx context.Context) *cobra.Command {
	var enabled, yes bool
	var models string
	cmd := &cobra.Command{Use: "configure <provider>", Short: "Save discovery eligibility and unverified model declarations", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("enabled") && !cmd.Flags().Changed("models") {
			return fmt.Errorf("set --enabled or --models")
		}
		client, err := a.runtimeClient()
		if err != nil {
			return err
		}
		view, err := client.Providers(ctx)
		if err != nil {
			return err
		}
		if view.Revision == 0 {
			return fmt.Errorf("editing requires an updated runtime controller; rebuild and restart it")
		}
		var registration *ProviderRegistration
		for _, row := range view.Providers {
			if row.ID == args[0] {
				registration = &ProviderRegistration{ID: row.ID, Enabled: row.Enabled, Models: row.DeclaredModels}
				break
			}
		}
		if registration == nil {
			return fmt.Errorf("unsupported provider")
		}
		if cmd.Flags().Changed("enabled") {
			registration.Enabled = enabled
		}
		if cmd.Flags().Changed("models") {
			registration.Models = DeclaredProviderModels(models)
		}
		if registration.Models == nil {
			registration.Models = []string{}
		}
		lines := []string{"Declarations remain unverified. Enabling permits discovery on refresh; existing inference settings stay in use."}
		if !yes && !Confirm(a.stdin, cmd.OutOrStdout(), "Save registration?", lines) {
			return fmt.Errorf("nothing saved")
		}
		if _, err = client.ConfigureProvider(ctx, view.Revision, *registration); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Provider registration saved.")
		return nil
	}}
	cmd.Flags().BoolVar(&enabled, "enabled", true, "Allow discovery for this adapter")
	cmd.Flags().StringVar(&models, "models", "", "Comma-separated declared model names; an empty value clears declarations")
	cmd.Flags().BoolVar(&yes, "yes", false, "Save without asking")
	return cmd
}
