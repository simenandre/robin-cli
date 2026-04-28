package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Org       string           `json:"org"`
	Email     string           `json:"email"`
	Password  string           `json:"password"`
	QuickBook *QuickBookConfig `json:"quick_book,omitempty"`
}

type QuickBookConfig struct {
	Location           int64  `json:"location"`
	Priority           []int  `json:"priority"`
	MinDurationMinutes int    `json:"min_duration_minutes"`
	MaxDurationMinutes int    `json:"max_duration_minutes"`
	WindowMinutes      int    `json:"window_minutes"`
	TimeZone           string `json:"time_zone"`
	Title              string `json:"title,omitempty"`
}

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "robin"), nil
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func Load() (*Config, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no config at %s — run `robin init` first", p)
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if c.Org == "" || c.Email == "" || c.Password == "" {
		return nil, fmt.Errorf("config %s is missing org/email/password", p)
	}
	return &c, nil
}

func Save(c *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	p, err := path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}
