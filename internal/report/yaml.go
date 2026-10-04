package report

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/0xdeafcafe/springclean/internal/domain"
	"gopkg.in/yaml.v3"
)

type Report struct {
	Version     int       `yaml:"version"`
	GeneratedAt time.Time `yaml:"generated_at"`
	Mode        string    `yaml:"mode"`
	Root        string    `yaml:"root,omitempty"`
	TotalBytes  int64     `yaml:"total_bytes"`
	TotalItems  int       `yaml:"total_items"`
	// WalkedBytes is everything the walk measured, listed below or not.
	WalkedBytes int64            `yaml:"walked_bytes,omitempty"`
	Unreadable  []string         `yaml:"unreadable,omitempty"`
	Suspects    []domain.Suspect `yaml:"suspects"`
}

const SchemaVersion = 1

func Build(result domain.ScanResult) Report {
	r := Report{
		Version:     SchemaVersion,
		GeneratedAt: result.FinishedAt,
		Mode:        result.Mode.String(),
		Root:        result.Root,
		WalkedBytes: result.Stats.WalkedBytes,
		Unreadable:  result.Stats.Unreadable,
		Suspects:    append([]domain.Suspect(nil), result.Suspects...),
	}
	sort.Slice(r.Suspects, func(i, j int) bool {
		return r.Suspects[i].Size > r.Suspects[j].Size
	})
	for _, s := range r.Suspects {
		r.TotalBytes += s.Size
		r.TotalItems++
	}
	return r
}

func Save(r Report, path string) error {
	data, err := yaml.Marshal(r)
	if err != nil {
		return err
	}
	header := []byte("# springclean report\n" +
		"# Edit `marked: true` to include items in the deletion plan.\n" +
		"# Run `springclean apply <this-file>` to move marked items to the Trash.\n\n")
	return os.WriteFile(path, append(header, data...), 0o644)
}

func Load(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var r Report
	if err := yaml.Unmarshal(data, &r); err != nil {
		return Report{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if r.Version != SchemaVersion {
		return Report{}, fmt.Errorf("unsupported report version %d (need %d)", r.Version, SchemaVersion)
	}
	return r, nil
}

// OpenInEditor launches $EDITOR (falling back to `vi`) on the report file
// and blocks until the editor exits.
func OpenInEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// MarkedPaths returns the paths of all suspects flagged for deletion.
func (r Report) MarkedPaths() []string {
	var out []string
	for _, s := range r.Suspects {
		if s.Marked {
			out = append(out, s.Path)
		}
	}
	return out
}

// MarkedBytes totals the size of all marked suspects.
func (r Report) MarkedBytes() int64 {
	var total int64
	for _, s := range r.Suspects {
		if s.Marked {
			total += s.Size
		}
	}
	return total
}
