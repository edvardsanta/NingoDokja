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
	cmd.Flags().StringVar(&a.runtimeRoot, "runtime-root", "", "Checkout that holds docker-compose.lite.yml")
	return cmd
}
