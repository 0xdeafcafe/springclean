package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xdeafcafe/springclean/internal/cache"
	"github.com/0xdeafcafe/springclean/internal/catalog"
	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/scan"
	"github.com/0xdeafcafe/springclean/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	scopeFlag       string
	rootFlag        string
	reportPathFlag  string
	yesFlag         bool
	manualTrashFlag bool
	worktreeAgeFlag int
	ignoredAgeFlag  int
	scanIgnoredFlag bool
	noPruneFlag     bool
	hereFlag        bool
	cacheAgeFlag    int
	noDedupeFlag    bool
	deleteFlag      bool
	freshFlag       bool
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "springclean",
		Short: "✿ spring-clean your disk — a TUI for finding & trashing the gunk",
		Long: "springclean walks your home directory and known macOS caches to find disk-space hogs,\n" +
			"then lets you review and mark items for the Trash, or delete them outright if you'd\n" +
			"rather skip the bin. Sizes are what deleting would actually free, so copies that\n" +
			"share storage are counted once rather than once each.",
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
	cmd.PersistentFlags().StringVar(&scopeFlag, "scope", "full", "scan scope: full | curated | home | root")
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
	cmd.PersistentFlags().BoolVar(&hereFlag, "here", false,
		"scan the current directory (shorthand for --scope=root --root=.)")
	cmd.PersistentFlags().IntVar(&cacheAgeFlag, "cache-age", 0,
		"hide build/dependency caches whose project was worked on in the last N days (0 shows all)")
	cmd.PersistentFlags().BoolVar(&noDedupeFlag, "no-dedupe", false,
		"skip working out which copies share storage (faster scan; counts every clone and hard link in full)")
	cmd.PersistentFlags().BoolVar(&deleteFlag, "delete", false,
		"delete outright instead of moving to the Trash (apply) — irreversible")

	cmd.PersistentFlags().BoolVar(&freshFlag, "fresh", false,
		"re-measure everything instead of reusing sizes remembered from scans in the last day")

	cmd.AddCommand(newWorktreesCmd())
	cmd.AddCommand(newScanCmd())
	cmd.AddCommand(newReviewCmd())
	cmd.AddCommand(newApplyCmd())
	return cmd
}

func Execute() {
	// Scans run for minutes; whatever the user is doing meanwhile comes first.
	scan.BeNice()
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func scanConfigFromFlags() (scan.Config, error) {
	mode, ok := domain.ParseScanMode(scopeFlag)
	if !ok {
		return scan.Config{}, fmt.Errorf("invalid --scope %q (want full|curated|home|root)", scopeFlag)
	}
	cfg := scan.Config{
		Mode:            mode,
		WorktreeAgeDays: worktreeAgeFlag,
		IgnoredAgeDays:  ignoredAgeFlag,
		ScanIgnored:     scanIgnoredFlag,
		CacheAgeDays:    cacheAgeFlag,
		SkipDedupe:      noDedupeFlag,
		CachePath:       cachePath(),
	}

	// --here is shorthand for pointing --scope=root at the working directory.
	if hereFlag && rootFlag == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return scan.Config{}, fmt.Errorf("cannot resolve the working directory: %w", err)
		}
		cfg.Mode = domain.ModeRoot
		rootFlag = cwd
	} else if hereFlag {
		cfg.Mode = domain.ModeRoot
	}

	if cfg.Mode == domain.ModeRoot {
		root, err := resolveRoot(rootFlag)
		if err != nil {
			return scan.Config{}, err
		}
		cfg.Root = root
	}
	return cfg, nil
}

// resolveRoot turns whatever the user typed into a directory that exists.
//
// A leading `~` is expanded here because the shell won't: in both bash and zsh
// `--root=~/code` passes the tilde through literally, and a scan of a path
// that doesn't exist used to succeed while quietly reporting nothing at all.
func resolveRoot(p string) (string, error) {
	if p == "" {
		return "/", nil
	}
	p = catalog.ExpandHome(p)
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("cannot resolve --root %q: %w", p, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("--root %q does not exist", abs)
		}
		return "", fmt.Errorf("cannot read --root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--root %q is not a directory", abs)
	}
	return abs, nil
}

func runTUI(opts tui.Options) error {
	p := tea.NewProgram(tui.New(opts), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func cachePath() string {
	if freshFlag {
		return ""
	}
	return cache.LumpsPath()
}
