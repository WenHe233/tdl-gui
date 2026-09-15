package app

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	Root         string
	Database     string
	TDLStorage   string
	TDLProtected string
	Engines      string
	Cache        string
	Staging      string
	Logs         string
}

func ResolvePaths(override string) (Paths, error) {
	root := override
	if root == "" {
		root = os.Getenv("TDL_GUI_HOME")
	}
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user config directory: %w", err)
		}
		root = filepath.Join(base, "TDL Media")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve data directory: %w", err)
	}
	p := Paths{
		Root:         abs,
		Database:     filepath.Join(abs, "state.db"),
		TDLStorage:   filepath.Join(abs, "tdl", "sessions.json"),
		TDLProtected: filepath.Join(abs, "tdl", "sessions.dpapi"),
		Engines:      filepath.Join(abs, "engines"),
		Cache:        filepath.Join(abs, "cache"),
		Staging:      filepath.Join(abs, "staging"),
		Logs:         filepath.Join(abs, "logs"),
	}
	for _, dir := range []string{p.Root, filepath.Dir(p.TDLStorage), p.Engines, p.Cache, p.Staging, p.Logs} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Paths{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return p, nil
}
