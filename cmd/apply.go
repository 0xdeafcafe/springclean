package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/report"
	"github.com/0xdeafcafe/springclean/internal/trash"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <report.yaml>",
		Short: "Move every `marked: true` item from the report to the Trash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := report.Load(args[0])
			if err != nil {
				return err
			}
			paths := r.MarkedPaths()
			if len(paths) == 0 {
				fmt.Fprintln(os.Stderr, "nothing marked. Edit the report and set `marked: true` on items you want trashed.")
				return nil
			}

			printDryRun(r)

			if !yesFlag {
				fmt.Fprintf(os.Stderr, "\nMove %d items (%s) to the Trash? [y/N] ",
					len(paths), humanize.Bytes(uint64(r.MarkedBytes())))
				ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if !isYes(ans) {
					fmt.Fprintln(os.Stderr, "cancelled.")
					return nil
				}
			}

			res := trash.MoveMany(paths)
			if automationDenied(res) && !manualTrashFlag {
				printAutomationHelp()
				return errors.New("automation denied; rerun with --manual-trash or grant permission and retry")
			}
			if manualTrashFlag {
				res = trash.MoveManyManual(paths)
			}
			fmt.Fprintf(os.Stderr, "\n✿  trashed %d items via %s\n", len(res.Trashed), res.Method)
			if len(res.Skipped) > 0 {
				fmt.Fprintf(os.Stderr, "  %d items already gone\n", len(res.Skipped))
			}
			if len(res.Failed) > 0 {
				fmt.Fprintf(os.Stderr, "  %d items failed:\n", len(res.Failed))
				for p, e := range res.Failed {
					fmt.Fprintf(os.Stderr, "    - %s: %v\n", p, e)
				}
				return fmt.Errorf("%d failures", len(res.Failed))
			}
			return nil
		},
	}
}

func automationDenied(res trash.Result) bool {
	if len(res.Failed) == 0 {
		return false
	}
	for _, err := range res.Failed {
		if !trash.IsAutomationDenied(err) {
			return false
		}
	}
	return true
}

func printAutomationHelp() {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "⚠  macOS blocked springclean from talking to Finder (TCC Automation).")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "To grant permission (preserves Finder's \"Put Back\"):")
	fmt.Fprintln(os.Stderr, "  1. System Settings → Privacy & Security → Automation")
	fmt.Fprintln(os.Stderr, "  2. Find your terminal app (Terminal, iTerm, Ghostty, …)")
	fmt.Fprintln(os.Stderr, "  3. Toggle ✓ next to \"Finder\"")
	fmt.Fprintln(os.Stderr, "  4. Rerun this apply")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Or run with --manual-trash to move items directly to ~/.Trash")
	fmt.Fprintln(os.Stderr, "(no Finder, no \"Put Back\" metadata).")
}

func printDryRun(r report.Report) {
	byCat := map[domain.Category]struct {
		count int
		bytes int64
	}{}
	for _, s := range r.Suspects {
		if !s.Marked {
			continue
		}
		v := byCat[s.Category]
		v.count++
		v.bytes += s.Size
		byCat[s.Category] = v
	}
	fmt.Fprintln(os.Stderr, "✿  spring cleaning plan")
	fmt.Fprintln(os.Stderr, strings.Repeat("─", 50))
	var totalCount int
	var totalBytes int64
	for _, c := range domain.AllCategories() {
		v, ok := byCat[c]
		if !ok || v.count == 0 {
			continue
		}
		fmt.Fprintf(os.Stderr, "  %s %-14s %4d items · %s\n",
			c.Glyph(), c.Label(), v.count, humanize.Bytes(uint64(v.bytes)))
		totalCount += v.count
		totalBytes += v.bytes
	}
	fmt.Fprintln(os.Stderr, strings.Repeat("─", 50))
	fmt.Fprintf(os.Stderr, "  total: %d items · %s\n", totalCount, humanize.Bytes(uint64(totalBytes)))
}

func isYes(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "y" || s == "yes"
}
