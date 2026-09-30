package cmd

import (
	"fmt"
	"os"

	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/scan"
	"github.com/0xdeafcafe/springclean/internal/tui"
	"github.com/spf13/cobra"
)

func newWorktreesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "worktrees [path]",
		Short: "Find stale git worktrees under a path (defaults to the current directory)",
		Long: "Walks a directory looking for linked git worktrees nobody has touched\n" +
			"in a while, and opens them in the TUI with the worktree filter applied.\n\n" +
			"Equivalent to: springclean --scope=root --root=<path>",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			if target == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("cannot resolve the working directory: %w", err)
				}
				target = cwd
			}
			root, err := resolveRoot(target)
			if err != nil {
				return err
			}

			cfg := scan.Config{
				Mode:            domain.ModeRoot,
				Root:            root,
				WorktreeAgeDays: worktreeAgeFlag,
				IgnoredAgeDays:  ignoredAgeFlag,
				ScanIgnored:     scanIgnoredFlag,
				CachePath:       cachePath(),
			}
			return runTUI(tui.Options{
				ScanConfig: cfg,
				ReportPath: reportPathFlag,
				// Land on the worktree category so the result is the answer to
				// the question that was asked, not a list to filter down.
				InitialFilter: domain.CatGitWorktree,
				SkipSplash:    true,
			})
		},
	}
}
