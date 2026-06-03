package scan

import (
	"errors"
	"io/fs"
	"os"

	"github.com/0xdeafcafe/springclean/internal/catalog"
)

type FDAStatus struct {
	Granted        bool
	BlockedPaths   []string
	ProbedPaths    []string
}

// ProbeFullDiskAccess attempts to read a handful of protected directories.
// If any probe returns a permission error, FDA is not granted.
func ProbeFullDiskAccess() FDAStatus {
	probes := catalog.FDAProbePaths()
	status := FDAStatus{Granted: true, ProbedPaths: probes}
	for _, p := range probes {
		if _, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if errors.Is(err, fs.ErrPermission) {
				status.Granted = false
				status.BlockedPaths = append(status.BlockedPaths, p)
			}
			continue
		}
		_, err := os.ReadDir(p)
		if err != nil && errors.Is(err, fs.ErrPermission) {
			status.Granted = false
			status.BlockedPaths = append(status.BlockedPaths, p)
		}
	}
	return status
}
