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

// ChatBinding reads selection only; it does not invoke a provider.
func (c *RuntimeClient) ChatBinding(ctx context.Context) (ChatBinding, error) {
	var result ChatBinding
	if err := c.call(ctx, "runtime.chat", nil, &result); err != nil {
		return result, err
	}
	if result.Revision < 1 || result.Revision > 9007199254740991 || (result.Provider == "") != (result.Model == "") {
		return ChatBinding{}, fmt.Errorf("incompatible chat configuration; rebuild and restart the controller")
	}
	return result, nil
}

func (c *RuntimeClient) ConfigureChat(ctx context.Context, binding ChatBinding) (ChatBinding, error) {
	if binding.Revision < 1 || binding.Revision >= 9007199254740991 || binding.Provider == "" || binding.Model == "" {
		return ChatBinding{}, fmt.Errorf("invalid chat selection")
	}
	var result ChatBinding
	err := c.call(ctx, "runtime.chat.configure", map[string]any{"configuration_revision": binding.Revision, "provider": binding.Provider, "model": binding.Model}, &result)
	if err != nil {
		return result, err
	}
	if result.Revision != binding.Revision+1 || result.Provider != binding.Provider || result.Model != binding.Model {
		return ChatBinding{}, fmt.Errorf("incompatible chat save response; reload configuration")
	}
	return result, nil
}

func (a *App) newChatBindingCommand(ctx context.Context) *cobra.Command {
	return &cobra.Command{Use: "chat <provider> <model>", Short: "Select the registered provider and declared model used for chat", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := a.runtimeClient()
		if err != nil {
			return err
		}
		b, err := c.ChatBinding(ctx)
		if err != nil {
			return err
		}
		b.Provider, b.Model = args[0], args[1]
		b, err = c.ConfigureChat(ctx, b)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Chat uses %s / %s. Inference has not been tested.\n", b.Provider, b.Model)
		return nil
	}}
}
