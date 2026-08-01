package cmd

import (
	"fmt"
	"os"

	"github.com/0xdeafcafe/springclean/internal/catalog"
	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/scan"
	"github.com/0xdeafcafe/springclean/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	scopeFlag      string
	rootFlag       string
	reportPathFlag string
	yesFlag        bool
	manualTrashFlag bool
	worktreeAgeFlag int
	ignoredAgeFlag  int
	scanIgnoredFlag bool
	noPruneFlag     bool
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "springclean",
		Short: "✿ spring-clean your disk — a TUI for finding & trashing the gunk",
		Long: "springclean walks your home directory and known macOS caches to find disk-space hogs,\n" +
			"then lets you review and mark items for the Trash. Move-to-trash only — never rm.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := scanConfigFromFlags()
			if err != nil {
				return err
			}
			return runTUI(tui.Options{
				ScanConfig: cfg,
				ReportPath: reportPathFlag,
			})
		},
	}
	cmd.PersistentFlags().StringVar(&scopeFlag, "scope", "curated", "scan scope: curated | home | root")
	cmd.PersistentFlags().StringVar(&rootFlag, "root", "", "root path when --scope=root (defaults to /)")
	cmd.PersistentFlags().StringVarP(&reportPathFlag, "report", "o", "springclean-report.yaml", "report file path")
	cmd.PersistentFlags().BoolVarP(&yesFlag, "yes", "y", false, "skip confirmation prompts (apply)")
	cmd.PersistentFlags().BoolVar(&manualTrashFlag, "manual-trash", false, "move to ~/.Trash directly (no Finder, no Put Back) — use when Automation is blocked")
	cmd.PersistentFlags().IntVar(&worktreeAgeFlag, "worktree-age", catalog.WorktreeAgeDays,
		"days a git worktree must sit untouched to be flagged (-1 to report all)")
	cmd.PersistentFlags().IntVar(&ignoredAgeFlag, "ignored-age", catalog.IgnoredCruftAgeDays,
		"days gitignored cruft must sit untouched to be flagged (-1 to report all)")
	cmd.PersistentFlags().BoolVar(&scanIgnoredFlag, "ignored", false,
		"ask git for large, stale gitignored files in every repo the scan crosses")
	cmd.PersistentFlags().BoolVar(&noPruneFlag, "no-prune", false,
		"skip `git worktree prune` after trashing worktrees")

	cmd.AddCommand(newScanCmd())
	cmd.AddCommand(newReviewCmd())
	cmd.AddCommand(newApplyCmd())
	return cmd
}

func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func scanConfigFromFlags() (scan.Config, error) {
	mode, ok := domain.ParseScanMode(scopeFlag)
	if !ok {
		return scan.Config{}, fmt.Errorf("invalid --scope %q (want curated|home|root)", scopeFlag)
	}
	cfg := scan.Config{
		Mode:            mode,
		WorktreeAgeDays: worktreeAgeFlag,
		IgnoredAgeDays:  ignoredAgeFlag,
		ScanIgnored:     scanIgnoredFlag,
	}
	if mode == domain.ModeRoot {
		if rootFlag == "" {
			cfg.Root = "/"
		} else {
			cfg.Root = rootFlag
		}
	}
	return cfg, nil
}

func runTUI(opts tui.Options) error {
	p := tea.NewProgram(tui.New(opts), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
