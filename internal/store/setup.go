package store

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

var ErrAlreadyInitialized = errors.New("site already initialized")

// InitializeSite serializes the empty-site check with user creation, including
// ordinary registrations, and commits the administrator and settings together.
func (s *Store) InitializeSite(ctx context.Context, username, password, email, siteName string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `LOCK TABLE users IN EXCLUSIVE MODE`); err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrAlreadyInitialized
	}
	u, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users (username, password_hash, email, group_id)
		VALUES ($1,$2,$3,1) RETURNING `+userCols, username, string(hash), email))
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO settings (key,value) VALUES ('site_name',$1)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, siteName); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	settingsMu.Lock()
	settingsCache = nil
	settingsMu.Unlock()
	return u, nil
}
