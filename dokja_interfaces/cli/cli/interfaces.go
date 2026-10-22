package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

// linkPoll is how often `interfaces link` reads the link state while it waits for a scan.
const linkPoll = 2 * time.Second

func (a *App) linkRequest(ctx context.Context, eventType, name string) (LinkView, error) {
	response, err := a.call(ctx, a.baseEvent(eventType, map[string]any{"interface": name}))
	if err != nil {
		return LinkView{}, err
	}
	view := LinkView{Interface: name}
	if state, err := domainField(response.Result, "state"); err == nil {
		view.State, _ = state.(string)
	}
	if matrix, err := domainField(response.Result, "matrix"); err == nil {
		view = ParseLink(map[string]any{"interface": name, "state": view.State, "matrix": matrix})
	}
	return view, nil
}

func (a *App) newInterfacesCommand(ctx context.Context) *cobra.Command {
	command := &cobra.Command{
		Use:   "interfaces",
		Short: "Link the interfaces that sign in to an account (WhatsApp)",
		Long: "Needs DOKJA_COMMUNICATIONS_TOKEN, the credential the orchestrator and the desktop share. " +
			"The code to scan is drawn in the terminal and never printed as text.",
	}

	var fresh bool
	var limit time.Duration
	link := &cobra.Command{
		Use:       "link <interface>",
		Short:     "Show the code to scan and wait until the account is linked",
		Args:      cobra.ExactArgs(1),
		ValidArgs: LinkableInterfaces,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkLinkable(args[0]); err != nil {
				return err
			}
			if fresh {
				if _, err := a.linkRequest(ctx, "communications.link.start", args[0]); err != nil {
					return err
				}
			}
			return a.watchLink(ctx, cmd.OutOrStdout(), args[0], limit)
		},
	}
	link.Flags().BoolVar(&fresh, "new", false, "Ask for a new code first")
	link.Flags().DurationVar(&limit, "wait", 3*time.Minute, "How long to wait for the scan")

	var yes bool
	unlink := &cobra.Command{
		Use:       "unlink <interface>",
		Short:     "Unlink the account and erase its saved session",
		Args:      cobra.ExactArgs(1),
		ValidArgs: LinkableInterfaces,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkLinkable(args[0]); err != nil {
				return err
			}
			if !yes && !Confirm(a.stdin, cmd.OutOrStdout(), "Unlink "+args[0]+"?", []string{"The account is unlinked and its saved session erased; linking again needs a new scan."}) {
				return fmt.Errorf("%s not unlinked", args[0])
			}
			view, err := a.linkRequest(ctx, "communications.link.reset", args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", args[0], LinkStateText(view.State))
			return nil
		},
	}
	unlink.Flags().BoolVar(&yes, "yes", false, "Unlink without asking")

	command.AddCommand(link, unlink)
	return command
}

// watchLink draws each new code and returns once the account is linked or the wait is over.
func (a *App) watchLink(ctx context.Context, out io.Writer, name string, limit time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var shown []string
	lastState := ""
	for {
		view, err := a.linkRequest(ctx, "communications.link.status", name)
		if err != nil {
			return err
		}
		if view.State == "connected" {
			fmt.Fprintf(out, "%s: %s\n", name, LinkStateText(view.State))
			return nil
		}
		if len(view.Matrix) > 0 && !sameRows(view.Matrix, shown) {
			code, err := RenderLinkCode(view.Matrix)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "\nScan with the phone: WhatsApp > Linked devices > Link a device\n%s", code)
			shown = view.Matrix
		}
		if view.State != lastState {
			fmt.Fprintf(out, "%s: %s\n", name, LinkStateText(view.State))
			lastState = view.State
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s is not linked yet (%s)", name, LinkStateText(lastState))
		case <-time.After(linkPoll):
		}
	}
}

func sameRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
