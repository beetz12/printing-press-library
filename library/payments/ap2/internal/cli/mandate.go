package cli

import (
	"github.com/spf13/cobra"
)

// newMandateCmd returns the parent "mandate" command.
// Registers newMandateSignCmd (US-002) and newMandateVerifyCmd (US-003).
func newMandateCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mandate",
		Short: "Sign and verify AP2 mandate envelopes",
		Long: `mandate — tools for AP2 FinalizationEnvelopes emitted by ucp-pp-cli.

Subcommands:
  verify   Verify signature and chain integrity of a signed envelope
  sign     Sign an unsigned envelope (added by US-002)`,
	}
	cmd.AddCommand(newMandateSignCmd(flags))
	cmd.AddCommand(newMandateVerifyCmd(flags))
	return cmd
}
