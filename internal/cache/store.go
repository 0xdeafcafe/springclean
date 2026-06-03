package cache

import (
	"encoding/gob"
	"errors"
	"os"
	"path/filepath"

	"github.com/0xdeafcafe/springclean/internal/domain"
)

const fileName = "last_scan.gob"

func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "Application Support", "springclean")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

func Save(r domain.ScanResult) error {
	p, err := Path()
	if err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return gob.NewEncoder(f).Encode(r)
}

func Load() (domain.ScanResult, bool, error) {
	p, err := Path()
	if err != nil {
		return domain.ScanResult{}, false, err
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.ScanResult{}, false, nil
		}
		return domain.ScanResult{}, false, err
	}
	defer f.Close()
	var r domain.ScanResult
	if err := gob.NewDecoder(f).Decode(&r); err != nil {
		return domain.ScanResult{}, false, err
	}
	return r, true, nil
}
