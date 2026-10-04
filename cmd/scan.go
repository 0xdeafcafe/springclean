package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/0xdeafcafe/springclean/internal/cache"
	"github.com/0xdeafcafe/springclean/internal/disk"
	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/report"
	"github.com/0xdeafcafe/springclean/internal/scan"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

func newScanCmd() *cobra.Command {
	var printSum bool
	var f summaryFilter
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Run a scan and write a YAML report (non-interactive)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := scanConfigFromFlags()
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✿  springclean — scanning (%s)\n", cfg.Mode)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ch := scan.New(cfg).Run(ctx)
			var result domain.ScanResult
			lastPrint := time.Now()
			for ev := range ch {
				switch e := ev.(type) {
				case scan.ProgressEvent:
					if time.Since(lastPrint) > 500*time.Millisecond {
						fmt.Fprintf(os.Stderr, "\r\033[K  %d items · %d suspects · %s reclaimable",
							e.P.ItemsSeen,
							e.P.SuspectCount,
							humanize.Bytes(uint64(e.P.SuspectBytes)),
						)
						lastPrint = time.Now()
					}
				case scan.DoneEvent:
					result = e.R
				}
			}
			fmt.Fprintln(os.Stderr)
			if blocked := disk.TimedOut(); len(blocked) > 0 {
				fmt.Fprintf(os.Stderr, "  %d folders left out, macOS was waiting on a privacy prompt for them:\n", len(blocked))
				for _, p := range blocked {
					fmt.Fprintf(os.Stderr, "    - %s\n", p)
				}
			}

			_ = cache.Save(result)
			r := report.Build(result)
			if err := report.Save(r, reportPathFlag); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "✿  found %d suspects, %s reclaimable → %s\n",
				len(r.Suspects),
				humanize.Bytes(uint64(r.TotalBytes)),
				reportPathFlag,
			)
			fmt.Fprintln(os.Stderr, "  open the report, mark items with `marked: true`, then run:")
			fmt.Fprintf(os.Stderr, "  springclean apply %s\n", reportPathFlag)
			if printSum || f.JSON {
				return printSummary(os.Stdout, r, f, time.Now())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&printSum, "summary", false,
		"print the ranked digest to stdout once the scan finishes (see `springclean summary`)")
	f.register(cmd)
	return cmd
}
