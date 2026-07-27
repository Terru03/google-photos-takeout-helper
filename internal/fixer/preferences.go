package fixer

import (
	"encoding/json"
	"errors"
	"os"
)

type Preferences struct {
	LastInputPath   string         `json:"lastInputPath,omitempty"`
	LastOutputPath  string         `json:"lastOutputPath,omitempty"`
	LastStagingPath string         `json:"lastStagingPath,omitempty"`
	ZipRoots        []string       `json:"zipRoots,omitempty"`
	WorkPaths       []string       `json:"workPaths,omitempty"`
	Options         ProcessOptions `json:"options"`
}

func LoadPreferences() (Preferences, error) {
	paths, err := ResolveRuntimePaths("")
	if err != nil {
		return Preferences{}, err
	}

	data, err := os.ReadFile(paths.PreferencesPath)
	if errors.Is(err, os.ErrNotExist) {
		return Preferences{}, nil
	}
	if err != nil {
		return Preferences{}, err
	}

	var prefs Preferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return Preferences{}, err
	}
	prefs.Options = prefs.Options.Normalized()
	return prefs, nil
}

func SavePreferences(prefs Preferences) error {
	paths, err := ResolveRuntimePaths("")
	if err != nil {
		return err
	}
	if err := EnsureDir(paths.ConfigDir); err != nil {
		return err
	}

	prefs.Options = prefs.Options.Normalized()
	body, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(paths.PreferencesPath, body, 0o644)
}
