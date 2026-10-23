package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type ProviderRow struct {
	Enabled         bool     `json:"enabled"`
	DeclaredModels  []string `json:"declared_models"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	State           string   `json:"state"`
	Reason          string   `json:"reason"`
	Models          []string `json:"models"`
	ModelDiscovery  string   `json:"model_discovery"`
	InferenceStatus string   `json:"inference_status"`
	Truncated       bool     `json:"models_truncated"`
}
type ProvidersView struct {
	Revision  uint64        `json:"configuration_revision"`
	Providers []ProviderRow `json:"providers"`
	CheckedAt time.Time     `json:"checked_at"`
}

func (c *RuntimeClient) Providers(ctx context.Context) (ProvidersView, error) {
	var view *ProvidersView
	if err := c.call(ctx, "runtime.providers", nil, &view); err != nil {
		return ProvidersView{}, err
	}
	if view == nil {
		return ProvidersView{}, fmt.Errorf("the local runtime controller returned no provider inventory")
	}
	return *view, nil
}
func printProviders(out io.Writer, v ProvidersView) {
	fmt.Fprintln(out, "Discovery only; authentication and inference have not been tested.")
	for _, row := range v.Providers {
		fmt.Fprintf(out, "%s: %s", row.Name, row.State)
		if row.Reason != "" {
			fmt.Fprintf(out, " (%s)", row.Reason)
		}
		fmt.Fprintln(out)
		if v.Revision != 0 {
			fmt.Fprintf(out, "  Discovery enabled: %t.\n", row.Enabled)
			if len(row.DeclaredModels) == 0 {
				fmt.Fprintln(out, "  No declared models.")
			} else {
				fmt.Fprintln(out, "  Declared models (unverified): "+strings.Join(row.DeclaredModels, ", "))
			}
		}
		if row.ModelDiscovery == "listed" {
			if len(row.Models) == 0 {
				fmt.Fprintln(out, "  No local models.")
			} else {
				fmt.Fprintln(out, "  "+strings.Join(row.Models, ", "))
			}
			if row.Truncated {
				fmt.Fprintln(out, "  Model list truncated.")
			}
		} else {
			fmt.Fprintln(out, "  Model catalog unavailable.")
		}
	}
}
func (a *App) newProvidersCommand(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{Use: "providers", Short: "Inspect host CLIs and models in the local lite stack", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := a.runtimeClient()
		if err != nil {
			return err
		}
		view, err := client.Providers(ctx)
		if err != nil {
			return err
		}
		printProviders(cmd.OutOrStdout(), view)
		return nil
	}}
	cmd.PersistentFlags().StringVar(&a.runtimeRoot, "runtime-root", "", "Checkout that holds docker-compose.lite.yml")
	cmd.AddCommand(a.newProviderConfigurationCommand(ctx))
	return cmd
}
