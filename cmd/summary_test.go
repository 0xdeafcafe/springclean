package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/report"
)

var summaryNow = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func daysAgo(n int) time.Time { return summaryNow.Add(-time.Duration(n) * 24 * time.Hour) }

func summaryFixture() report.Report {
	return report.Report{
		Version: report.SchemaVersion,
		Mode:    "full",
		Suspects: []domain.Suspect{
			{ID: "A", Path: "/p/old/node_modules", Category: domain.CatDevCache, Size: 100, Shared: 900,
				LastUsed: daysAgo(200), Project: "/p/old", ProjectLastUsed: daysAgo(90)},
			{ID: "B", Path: "/p/live/node_modules", Category: domain.CatDevCache, Size: 300,
				LastUsed: daysAgo(200), Project: "/p/live", ProjectLastUsed: daysAgo(0)},
			{ID: "C", Path: "/p/wt", Category: domain.CatGitWorktree, Size: 500,
				LastUsed: daysAgo(40), Warning: "2 uncommitted files"},
			{ID: "D", Path: "/var/big", Category: domain.CatLargeDir, Size: 700},
		},
	}
}

func TestIdleDaysUsesTheCheckoutWhenItIsNewer(t *testing.T) {
	r := summaryFixture()
	if got := idleDays(r.Suspects[1], summaryNow); got != 0 {
		t.Fatalf("cache under a checkout touched today: idle = %d, want 0", got)
	}
	if got := idleDays(r.Suspects[0], summaryNow); got != 90 {
		t.Fatalf("idle = %d, want 90", got)
	}
	if got := idleDays(r.Suspects[3], summaryNow); got != -1 {
		t.Fatalf("undated item: idle = %d, want -1", got)
	}
}

func TestOlderThanDropsLiveAndUndatedItems(t *testing.T) {
	got := filterSuspects(summaryFixture().Suspects, summaryFilter{OlderThan: 30}, summaryNow)
	var ids []string
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "C,A" {
		t.Fatalf("got %v, want C then A", ids)
	}
}

func TestSummaryTotalsAndOrder(t *testing.T) {
	s := buildSummary(summaryFixture(), summaryFilter{Top: 2}, summaryNow)
	if s.Items != 4 || s.Frees != 1600 {
		t.Fatalf("items=%d frees=%d, want 4 and 1600", s.Items, s.Frees)
	}
	if len(s.Top) != 2 || s.Top[0].ID != "D" || s.Top[1].ID != "C" {
		t.Fatalf("top = %+v, want D then C", s.Top)
	}
	if s.Categories[0].Category != domain.CatLargeDir {
		t.Fatalf("first category = %s, want large_dir", s.Categories[0].Category)
	}
	for _, c := range s.Categories {
		if c.Category == domain.CatDevCache && (c.Frees != 400 || c.Footprint != 1300) {
			t.Fatalf("dev_cache frees=%d footprint=%d, want 400 and 1300", c.Frees, c.Footprint)
		}
	}
}

func TestSummaryCategoryFilterAndJSON(t *testing.T) {
	var buf bytes.Buffer
	f := summaryFilter{Categories: []string{"git_worktree"}, JSON: true}
	if err := printSummary(&buf, summaryFixture(), f, summaryNow); err != nil {
		t.Fatal(err)
	}
	var got summary
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if len(got.Top) != 1 || got.Top[0].Warning != "2 uncommitted files" || got.Top[0].IdleDays != 40 {
		t.Fatalf("top = %+v", got.Top)
	}
}

func TestSummaryTableShowsWarnings(t *testing.T) {
	var buf bytes.Buffer
	if err := printSummary(&buf, summaryFixture(), summaryFilter{}, summaryNow); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "/p/wt  [!2 uncommitted files]") {
		t.Fatalf("warning missing from table:\n%s", buf.String())
	}
}

func TestSetMarksByIDAndPath(t *testing.T) {
	r := summaryFixture()
	if missing := setMarks(&r, []string{"A", "/p/wt/"}, true); missing != nil {
		t.Fatalf("missing = %v", missing)
	}
	if !r.Suspects[0].Marked || !r.Suspects[2].Marked || r.Suspects[1].Marked {
		t.Fatalf("wrong marks: %+v", r.Suspects)
	}
	if r.MarkedBytes() != 600 {
		t.Fatalf("marked bytes = %d, want 600", r.MarkedBytes())
	}
}

func TestSetMarksChangesNothingWhenAKeyIsUnknown(t *testing.T) {
	r := summaryFixture()
	missing := setMarks(&r, []string{"A", "/nope"}, true)
	if len(missing) != 1 || missing[0] != "/nope" {
		t.Fatalf("missing = %v", missing)
	}
	if r.Suspects[0].Marked {
		t.Fatal("A was marked even though the call was refused")
	}
}
