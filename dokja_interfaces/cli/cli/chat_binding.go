package cli

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
)

type ChatBinding struct {
	Revision uint64 `json:"configuration_revision"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (a *App) newChatBindingCommand(ctx context.Context) *cobra.Command {
	return &cobra.Command{Use: "chat <provider> <model>", Short: "Select the registered provider and declared model used for chat", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := a.runtimeClient()
		if err != nil {
			return err
		}
		var b ChatBinding
		if err = c.call(ctx, "runtime.chat", nil, &b); err != nil {
			return err
		}
		if b.Revision < 1 {
			return fmt.Errorf("updated runtime controller required")
		}
		if err = c.call(ctx, "runtime.chat.configure", map[string]any{"configuration_revision": b.Revision, "provider": args[0], "model": args[1]}, &b); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Chat uses %s / %s. Inference has not been tested.\n", b.Provider, b.Model)
		return nil
	}}
}
