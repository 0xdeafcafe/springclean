package scan

import (
	"fmt"
	"time"

	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/simctl"
)

func (s *Scanner) detectSimRuntimes() {
	s.submit(func() {
		for _, r := range simctl.List(s.ctx) {
			if !r.Deletable || r.Path == "" {
				continue
			}
			reason := fmt.Sprintf("%s %s simulator runtime", r.Platform(), r.Version)
			if !r.LastUsedAt.IsZero() {
				reason += fmt.Sprintf(", last used %d days ago", int(time.Since(r.LastUsedAt).Hours()/24))
			}
			s.emit(domain.Suspect{
				ID:          domain.MakeID(r.Path),
				Path:        r.Path,
				Category:    domain.CatSimRuntime,
				Reason:      reason,
				IsDir:       true,
				Size:        r.SizeBytes,
				Apparent:    r.SizeBytes,
				LastUsed:    r.LastUsedAt,
				Regenerable: true, // re-downloadable from Xcode
			})
		}
	})
}
