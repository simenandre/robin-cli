package robin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/simenandre/robin-user-api/internal/config"
)

type Session struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	AccountID   int64     `json:"account_id,omitempty"`
	SavedAt     time.Time `json:"saved_at"`
}

func sessionPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

func LoadSession() (*Session, error) {
	p, err := sessionPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func SaveSession(s *Session) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	p, err := sessionPath()
	if err != nil {
		return err
	}
	s.SavedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}
