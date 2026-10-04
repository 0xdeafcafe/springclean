package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/report"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

type summaryFilter struct {
	Top        int
	Categories []string
	OlderThan  int
	JSON       bool
}

func (f *summaryFilter) register(cmd *cobra.Command) {
	cmd.Flags().IntVar(&f.Top, "top", 40, "how many items to list, largest first (0 lists all)")
	cmd.Flags().StringSliceVar(&f.Categories, "category", nil,
		"only these categories, comma separated (dev_cache, git_worktree, large_dir, ...)")
	cmd.Flags().IntVar(&f.OlderThan, "older-than", 0,
		"only items idle for at least N days; items with no known date are dropped")
	cmd.Flags().BoolVar(&f.JSON, "json", false, "print JSON instead of a table")
}

func newSummaryCmd() *cobra.Command {
	var f summaryFilter
	cmd := &cobra.Command{
		Use:   "summary <report.yaml>",
		Short: "Print a ranked plain-text or JSON digest of a report (non-interactive)",
		Long: "Reads a report written by `springclean scan` and prints totals per category and the\n" +
			"largest items, without opening the TUI. Meant for scripts and coding agents: scan once,\n" +
			"then slice the same report with --category and --older-than as often as needed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := report.Load(args[0])
			if err != nil {
				return err
			}
			return printSummary(os.Stdout, r, f, time.Now())
		},
	}
	f.register(cmd)
	return cmd
}

func newMarkCmd() *cobra.Command {
	var unmark bool
	cmd := &cobra.Command{
		Use:   "mark <report.yaml> <id-or-path>...",
		Short: "Set `marked: true` on report items by id or path, ready for apply",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := report.Load(args[0])
			if err != nil {
				return err
			}
			missing := setMarks(&r, args[1:], !unmark)
			if len(missing) > 0 {
				return fmt.Errorf("not in the report: %s", strings.Join(missing, ", "))
			}
			if err := report.Save(r, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✿  %d items marked, %s\n",
				len(r.MarkedPaths()), humanize.Bytes(uint64(r.MarkedBytes())))
			return nil
		},
	}
	cmd.Flags().BoolVar(&unmark, "unmark", false, "clear the mark instead of setting it")
	return cmd
}

// setMarks flips Marked on every suspect named by id or path, and returns the
// keys that matched nothing. Nothing is changed unless every key matches.
func setMarks(r *report.Report, keys []string, marked bool) []string {
	index := map[string]int{}
	for i, s := range r.Suspects {
		index[s.ID] = i
		index[s.Path] = i
	}
	var missing []string
	for _, k := range keys {
		if _, ok := index[strings.TrimRight(k, "/")]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return missing
	}
	for _, k := range keys {
		r.Suspects[index[strings.TrimRight(k, "/")]].Marked = marked
	}
	return nil
}

// lastTouched is the most recent sign of life for an item: its own date, or
// the date its checkout was last worked on when that is newer. A node_modules
// installed in spring under a branch edited this morning is not idle.
func lastTouched(s domain.Suspect) time.Time {
	if s.ProjectLastUsed.After(s.LastUsed) {
		return s.ProjectLastUsed
	}
	return s.LastUsed
}

// idleDays returns -1 when the item carries no date at all.
func idleDays(s domain.Suspect, now time.Time) int {
	t := lastTouched(s)
	if t.IsZero() {
		return -1
	}
	return int(now.Sub(t).Hours() / 24)
}

func filterSuspects(all []domain.Suspect, f summaryFilter, now time.Time) []domain.Suspect {
	want := map[string]bool{}
	for _, c := range f.Categories {
		want[strings.TrimSpace(c)] = true
	}
	var out []domain.Suspect
	for _, s := range all {
		if len(want) > 0 && !want[string(s.Category)] {
			continue
		}
		if f.OlderThan > 0 {
			if idle := idleDays(s, now); idle < f.OlderThan {
				continue
			}
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

type categoryTotal struct {
	Category  domain.Category `json:"category"`
	Items     int             `json:"items"`
	Frees     int64           `json:"frees_bytes"`
	Footprint int64           `json:"footprint_bytes"`
}

type summaryItem struct {
	ID          string          `json:"id"`
	Path        string          `json:"path"`
	Category    domain.Category `json:"category"`
	Frees       int64           `json:"frees_bytes"`
	Footprint   int64           `json:"footprint_bytes"`
	IdleDays    int             `json:"idle_days"`
	LastTouched *time.Time      `json:"last_touched,omitempty"`
	Regenerable bool            `json:"regenerable"`
	Marked      bool            `json:"marked"`
	Project     string          `json:"project,omitempty"`
	Reason      string          `json:"reason"`
	Warning     string          `json:"warning,omitempty"`
}

type summary struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Mode        string          `json:"mode"`
	Root        string          `json:"root,omitempty"`
	Items       int             `json:"items"`
	Frees       int64           `json:"frees_bytes"`
	WalkedBytes int64           `json:"walked_bytes,omitempty"`
	Unreadable  []string        `json:"unreadable,omitempty"`
	Categories  []categoryTotal `json:"categories"`
	Top         []summaryItem   `json:"top"`
}

func buildSummary(r report.Report, f summaryFilter, now time.Time) summary {
	picked := filterSuspects(r.Suspects, f, now)
	out := summary{GeneratedAt: r.GeneratedAt, Mode: r.Mode, Root: r.Root, Items: len(picked),
		WalkedBytes: r.WalkedBytes, Unreadable: r.Unreadable}

	byCat := map[domain.Category]*categoryTotal{}
	for _, s := range picked {
		out.Frees += s.Size
		t := byCat[s.Category]
		if t == nil {
			t = &categoryTotal{Category: s.Category}
			byCat[s.Category] = t
		}
		t.Items++
		t.Frees += s.Size
		t.Footprint += s.Footprint()
	}
	for _, t := range byCat {
		out.Categories = append(out.Categories, *t)
	}
	sort.Slice(out.Categories, func(i, j int) bool {
		a, b := out.Categories[i], out.Categories[j]
		if a.Frees != b.Frees {
			return a.Frees > b.Frees
		}
		return a.Category < b.Category
	})

	if f.Top > 0 && len(picked) > f.Top {
		picked = picked[:f.Top]
	}
	for _, s := range picked {
		item := summaryItem{
			ID: s.ID, Path: s.Path, Category: s.Category,
			Frees: s.Size, Footprint: s.Footprint(), IdleDays: idleDays(s, now),
			Regenerable: s.Regenerable, Marked: s.Marked,
			Project: s.Project, Reason: s.Reason, Warning: s.Warning,
		}
		if t := lastTouched(s); !t.IsZero() {
			item.LastTouched = &t
		}
		out.Top = append(out.Top, item)
	}
	return out
}

func printSummary(w io.Writer, r report.Report, f summaryFilter, now time.Time) error {
	s := buildSummary(r, f, now)
	if f.JSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}

	size := func(n int64) string { return humanize.Bytes(uint64(n)) }
	fmt.Fprintf(w, "springclean %s scan, %s: %d items, %s freed if all of it went\n\n",
		s.Mode, s.GeneratedAt.Format("2006-01-02 15:04"), s.Items, size(s.Frees))
	if s.WalkedBytes > 0 {
		fmt.Fprintf(w, "%s measured in all; the rest sits in folders too small to list.\n", size(s.WalkedBytes))
	}
	if len(s.Unreadable) > 0 {
		fmt.Fprintf(w, "%d folders could not be opened and are not counted (see `unreadable` in the report).\n",
			len(s.Unreadable))
	}
	if s.WalkedBytes > 0 || len(s.Unreadable) > 0 {
		fmt.Fprintln(w)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CATEGORY\tITEMS\tFREES\tFOOTPRINT")
	for _, c := range s.Categories {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", c.Category, c.Items, size(c.Frees), size(c.Footprint))
	}
	tw.Flush()

	fmt.Fprintf(w, "\nFREES is what deleting the item gives back. FOOTPRINT also counts storage it\n"+
		"shares with other items (APFS clones, hard links), so it only comes back once\n"+
		"every copy is gone. IDLE is days since the item or its checkout was last touched.\n\n")

	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FREES\tFOOTPRINT\tIDLE\tCATEGORY\tID\tPATH")
	for _, it := range s.Top {
		idle := "-"
		if it.IdleDays >= 0 {
			idle = fmt.Sprintf("%dd", it.IdleDays)
		}
		path := it.Path
		if it.Warning != "" {
			path += "  [!" + it.Warning + "]"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			size(it.Frees), size(it.Footprint), idle, it.Category, it.ID, path)
	}
	return tw.Flush()
}
