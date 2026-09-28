package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var (
	ErrEngagementInvalid   = errors.New("invalid engagement request")
	ErrEngagementConflict  = errors.New("engagement conflict")
	ErrEngagementForbidden = errors.New("engagement forbidden")
	ErrEngagementClosed    = errors.New("engagement closed or disabled")
)

type PollConfig struct {
	Enabled    bool `json:"enabled"`
	MaxOptions int  `json:"maxOptions"`
	MaxDays    int  `json:"maxDays"`
}
type BountyConfig struct {
	Enabled   bool  `json:"enabled"`
	MinPoints int64 `json:"minPoints"`
	MaxPoints int64 `json:"maxPoints"`
	MaxDays   int   `json:"maxDays"`
}
type CheckinConfig struct {
	Enabled    bool   `json:"enabled"`
	Experience int64  `json:"experience"`
	Points     int64  `json:"points"`
	TimeZone   string `json:"timeZone"`
}
type EngagementConfig struct {
	Version int64         `json:"version"`
	Poll    PollConfig    `json:"poll"`
	Bounty  BountyConfig  `json:"bounty"`
	Checkin CheckinConfig `json:"checkin"`
}

func (c EngagementConfig) Validate() error {
	if c.Version < 1 || c.Poll.MaxOptions < 2 || c.Poll.MaxOptions > 20 || c.Poll.MaxDays < 1 || c.Poll.MaxDays > 365 || c.Bounty.MinPoints < 1 || c.Bounty.MaxPoints < c.Bounty.MinPoints || c.Bounty.MaxPoints > 1000000 || c.Bounty.MaxDays < 1 || c.Bounty.MaxDays > 365 || c.Checkin.Experience < 0 || c.Checkin.Experience > 10000 || c.Checkin.Points < 0 || c.Checkin.Points > 10000 || !validReportTimeZone(c.Checkin.TimeZone) {
		return ErrEngagementInvalid
	}
	return nil
}
func engagementConfig(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, lock bool) (EngagementConfig, error) {
	var c EngagementConfig
	var raw []byte
	sql := `SELECT version,body FROM engagement_config WHERE id`
	if lock {
		sql += ` FOR SHARE`
	}
	if err := q.QueryRow(ctx, sql).Scan(&c.Version, &raw); err != nil {
		return c, err
	}
	version := c.Version
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	c.Version = version
	return c, c.Validate()
}
func (s *Store) EngagementConfig(ctx context.Context) (EngagementConfig, error) {
	return engagementConfig(ctx, s.pool, false)
}
func (s *Store) SaveEngagementConfig(ctx context.Context, c EngagementConfig, actor int64) error {
	if err := c.Validate(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var previous []byte
	var version int64
	if err = tx.QueryRow(ctx, `SELECT version,body FROM engagement_config WHERE id FOR UPDATE`).Scan(&version, &previous); err != nil {
		return err
	}
	if version != c.Version {
		return ErrEngagementConflict
	}
	var prior EngagementConfig
	if err = json.Unmarshal(previous, &prior); err != nil {
		return err
	}
	if prior.Checkin.TimeZone != c.Checkin.TimeZone {
		var used bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM checkin_records)`).Scan(&used); err != nil {
			return err
		}
		if used {
			return ErrEngagementConflict
		}
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE engagement_config SET version=version+1,body=$2 WHERE id AND version=$1`, c.Version, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrEngagementConflict
	}
	if err = pointsAudit(ctx, tx, actor, "engagement.configure", fmt.Sprintf("version=%d", c.Version+1)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func engagementActor(ctx context.Context, tx pgx.Tx, uid int64) error {
	var active bool
	err := tx.QueryRow(ctx, `SELECT NOT (coalesce(banned_until>now(),false) OR coalesce(blocked_until>now(),false) OR must_change_password) FROM users WHERE id=$1`, uid).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !active {
		return ErrEngagementForbidden
	}
	return err
}
func engagementText(s string, max int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, 0)
}
