package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// State represents the host-level runtime state.
type State struct {
	Initialized bool   `json:"initialized"`
	Users       []User `json:"users"`
}

// User describes a single managed macOS user.
type User struct {
	Name      string `json:"name"`
	Port      int    `json:"port"`
	Subdomain string `json:"subdomain"`
}

// Load reads the state from the given path (returns zero State if not exists).
func Load(path string) (State, error) {
	if path == "" {
		return State{}, errors.New("state path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read state: %w", err)
	}

	if len(data) == 0 {
		return State{}, nil
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("decode state: %w", err)
	}

	return s, nil
}

// Save writes the state to the given path atomically.
func Save(path string, s State) error {
	if path == "" {
		return errors.New("state path is empty")
	}

	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write temp state: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp state: %w", err)
	}

	return nil
}

func ensureDir(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	return nil
}
