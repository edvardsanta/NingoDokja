package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// modulePoll is how often --wait reads the controller again while an operation runs.
const modulePoll = 1200 * time.Millisecond

func (a *App) runtimeClient() (*RuntimeClient, error) {
	return LocalRuntimeClient(a.runtimeRoot, "cli", os.Getenv)
}

func (a *App) newModulesCommand(ctx context.Context) *cobra.Command {
	command := &cobra.Command{
		Use:   "modules",
		Short: "Start or stop local containers, including communication adapters",
		Long: "Talks to the local runtime controller, the same one the desktop uses, and starts it when " +
			"none is running. Start brings up a module with the dependencies it needs; stop keeps data and " +
			"dependencies still in use. Neither changes the orchestrator's service switches or jobs.",
	}
	command.PersistentFlags().StringVar(&a.runtimeRoot, "runtime-root", "", "Checkout that holds docker-compose.lite.yml (default: found from the current directory)")

	list := &cobra.Command{
		Use:   "list",
		Short: "Show local container lifecycle state (runtime.modules)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.runtimeClient()
			if err != nil {
				return err
			}
			snapshot, err := client.Request(ctx, "runtime.modules", nil)
			if err != nil {
				return err
			}
			return printJSON(snapshot)
		},
	}

	change := func(use, short string, enabled bool) *cobra.Command {
		var wait, yes bool
		command := &cobra.Command{
			Use:   use + " <module>",
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := a.runtimeClient()
				if err != nil {
					return err
				}
				if enabled && DeliveryModules[args[0]] && !yes {
					response, err := a.call(ctx, a.baseEvent("ningo.status", map[string]any{}))
					notice := DeliveryNotice(args[0], response.Result, err)
					if !Confirm(a.stdin, cmd.OutOrStdout(), "Start "+args[0]+"?", notice) {
						return fmt.Errorf("%s not started", args[0])
					}
				}
				snapshot, err := client.Request(ctx, "runtime.set", map[string]any{"module": args[0], "enabled": enabled})
				if err != nil {
					return err
				}
				if wait {
					if snapshot, err = waitForModules(ctx, client, snapshot); err != nil {
						return err
					}
				}
				if err := printJSON(snapshot); err != nil {
					return err
				}
				return moduleFailure(snapshot, args[0])
			},
		}
		command.Flags().BoolVar(&wait, "wait", false, "Wait until the operation finishes (it keeps running if this command is interrupted)")
		if enabled {
			command.Flags().BoolVar(&yes, "yes", false, "Start discord or the scheduler without asking (they answer or post with the switches and jobs already saved)")
		}
		return command
	}

	command.AddCommand(list,
		change("start", "Start a module and the dependencies it needs (runtime.set)", true),
		change("stop", "Stop a module, keeping its data (runtime.set)", false))
	return command
}

// waitForModules reads the controller until no operation is running.
func waitForModules(ctx context.Context, client *RuntimeClient, snapshot RuntimeSnapshot) (RuntimeSnapshot, error) {
	for snapshot.Busy {
		select {
		case <-ctx.Done():
			return snapshot, ctx.Err()
		case <-time.After(modulePoll):
		}
		var err error
		if snapshot, err = client.Request(ctx, "runtime.modules", nil); err != nil {
			return snapshot, err
		}
	}
	return snapshot, nil
}

func moduleFailure(snapshot RuntimeSnapshot, name string) error {
	for _, module := range snapshot.Modules {
		if module.Name == name && module.State == "failed" {
			return fmt.Errorf("module %s failed: %s", name, module.Error)
		}
	}
	return nil
}
