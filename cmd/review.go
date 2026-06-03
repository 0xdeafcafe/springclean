package cmd

import (
	"fmt"

	"github.com/0xdeafcafe/springclean/internal/report"
	"github.com/0xdeafcafe/springclean/internal/scan"
	"github.com/0xdeafcafe/springclean/internal/tui"
	"github.com/spf13/cobra"
)

func newReviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "review <report.yaml>",
		Short: "Open an existing report in the TUI to mark items",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			r, err := report.Load(path)
			if err != nil {
				return fmt.Errorf("load report: %w", err)
			}
			return runTUI(tui.Options{
				ScanConfig:      scan.Config{},
				StartFromReport: &r,
				ReportPath:      path,
			})
		},
	}
}
