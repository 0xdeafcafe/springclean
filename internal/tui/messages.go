package tui

import (
	"github.com/0xdeafcafe/springclean/internal/domain"
	"github.com/0xdeafcafe/springclean/internal/scan"
	"github.com/0xdeafcafe/springclean/internal/trash"
)

type tickMsg struct{}

type scanEventMsg struct{ ev scan.Event }

type scanClosedMsg struct{}

type fdaProbedMsg struct{ status scan.FDAStatus }

type applyDoneMsg struct {
	result trash.Result
	err    error
}

type errMsg struct{ err error }

type cachedScanLoadedMsg struct {
	result domain.ScanResult
	ok     bool
}
