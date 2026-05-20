// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/cliutil"
	"github.com/spf13/cobra"
)

// clusterIDRe matches Digg cluster identifiers: short slugs (e.g., "3vb8kiry")
// and full UUIDs (e.g., "8eee35ea-eb73-4291-b803-e7ed89df3fba"). Both surface
// as lowercase alphanumeric segments joined by hyphens. Underscores or upper
// case characters indicate a malformed input.
var clusterIDRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func newOpenCmd(flags *rootFlags) *cobra.Command {
	var launch bool

	cmd := &cobra.Command{
		Use:   "open <cluster-id>",
		Short: "Open a Digg story in the browser (prints URL by default; use --launch to open)",
		Example: `  digg-ai-pp-cli open 3vb8kiry
  digg-ai-pp-cli open 3vb8kiry --launch`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}

			clusterID := args[0]
			if !clusterIDRe.MatchString(clusterID) || len(clusterID) < 4 {
				return fmt.Errorf("invalid cluster ID %q: expected an 8-character slug or UUID (lowercase alphanumeric with optional hyphens)", clusterID)
			}
			url := "https://digg.com/ai/" + clusterID

			if cliutil.IsVerifyEnv() || !launch {
				fmt.Fprintf(cmd.OutOrStdout(), "would launch: %s\n", url)
				return nil
			}

			return openURL(url)
		},
	}

	cmd.Flags().BoolVar(&launch, "launch", false, "Actually open the URL in the default browser")

	return cmd
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported OS %q; open URL manually: %s", runtime.GOOS, url)
	}
	return cmd.Start()
}
