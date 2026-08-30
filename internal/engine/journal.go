package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ParkingLabel is the label given to the scratch tab a rebuild parks panes in.
// It exists so that a rebuild interrupted by a crash leaves something a human
// can recognise in the tab strip.
const ParkingLabel = "arrange:parking"

// journalName is the file, under the plugin's state directory, that records an
// in-flight rebuild.
const journalName = "parking.json"

// Journal records panes that are currently parked in a scratch tab.
//
// A rebuild moves panes out of a tab and back in again. If the process dies in
// between, those panes are still alive but sitting in a scratch tab the user
// never asked for. The journal is written before the first pane leaves and
// deleted once the last one is back, so `arrange drain` — wired to the plugin's
// startup hook — can put them home on the next server start.
type Journal struct {
	WorkspaceID  string    `json:"workspace_id"`
	HomeTabID    string    `json:"home_tab_id"`
	ScratchTabID string    `json:"scratch_tab_id"`
	Panes        []string  `json:"panes"`
	StartedAt    time.Time `json:"started_at"`

	// path is where the journal is stored. Empty disables journalling, which is
	// what happens outside herdr (in tests, or a manual run).
	path string
}

// JournalPath returns the journal's location for a state directory. An empty
// stateDir yields an empty path, which disables journalling.
func JournalPath(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, journalName)
}

func newJournal(stateDir, workspaceID, homeTabID string) *Journal {
	return &Journal{
		WorkspaceID: workspaceID,
		HomeTabID:   homeTabID,
		StartedAt:   time.Now().UTC(),
		path:        JournalPath(stateDir),
	}
}

// add records a pane as parked and persists the journal.
func (j *Journal) add(paneID string) error {
	j.Panes = append(j.Panes, paneID)
	return j.save()
}

// done records a pane as recovered and persists the journal.
func (j *Journal) done(paneID string) error {
	out := j.Panes[:0]
	for _, p := range j.Panes {
		if p != paneID {
			out = append(out, p)
		}
	}
	j.Panes = out
	return j.save()
}

func (j *Journal) save() error {
	if j.path == "" {
		return nil
	}
	body, err := json.Marshal(j)
	if err != nil {
		return err
	}
	// Write via a temporary file so a crash mid-write cannot leave a journal that
	// parses as valid but describes the wrong panes.
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write parking journal: %w", err)
	}
	if err := os.Rename(tmp, j.path); err != nil {
		return fmt.Errorf("write parking journal: %w", err)
	}
	return nil
}

// clear removes the journal, marking the rebuild complete.
func (j *Journal) clear() error {
	if j.path == "" {
		return nil
	}
	if err := os.Remove(j.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear parking journal: %w", err)
	}
	return nil
}

// loadJournal reads a journal. It returns nil, nil when there is none, which is
// the normal case.
func loadJournal(stateDir string) (*Journal, error) {
	path := JournalPath(stateDir)
	if path == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read parking journal: %w", err)
	}
	var j Journal
	if err := json.Unmarshal(body, &j); err != nil {
		// A corrupt journal is worse than none: it would send panes to a tab id
		// that may since have been reused. Drop it and fall back to the label scan.
		_ = os.Remove(path)
		return nil, fmt.Errorf("parking journal is corrupt, discarded: %w", err)
	}
	j.path = path
	return &j, nil
}
