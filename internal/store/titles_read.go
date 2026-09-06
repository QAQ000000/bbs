// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

type TitleSummary struct {
	ID    int64      `json:"id,string"`
	Name  string     `json:"name"`
	Badge LevelBadge `json:"badge"`
}

func (s *Store) EquippedTitles(ctx context.Context, ids []int64) (map[int64]*TitleSummary, error) {
	out := map[int64]*TitleSummary{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT e.user_id,t.id,t.body FROM title_equipment e JOIN user_titles u ON u.user_id=e.user_id AND u.title_id=e.title_id JOIN titles t ON t.id=e.title_id WHERE e.user_id=ANY($1) AND u.status='earned' AND (u.expires_at IS NULL OR u.expires_at>now()) AND t.body->>'status' IN ('active','paused')`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid, id int64
		var b []byte
		var c TitleDefinition
		if err = rows.Scan(&uid, &id, &b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &c); err != nil {
			return nil, err
		}
		out[uid] = &TitleSummary{ID: id, Name: c.Name, Badge: c.Badge}
	}
	return out, rows.Err()
}

type UserTitle struct {
	Title           TitleDefinition `json:"title"`
	Status          string          `json:"status"`
	Source          string          `json:"source,omitempty"`
	EarnedAt        *time.Time      `json:"earnedAt"`
	ExpiresAt       *time.Time      `json:"expiresAt"`
	EarnedVersion   int64           `json:"earnedVersion"`
	Equipped        bool            `json:"equipped"`
	Counts          []int64         `json:"counts"`
	CheckedAt       *time.Time      `json:"checkedAt"`
	ProgressPending bool            `json:"progressPending"`
}

func (s *Store) UserTitles(ctx context.Context, uid int64) ([]UserTitle, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, uid).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	defs, err := s.Titles(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT t.id,coalesce(u.status,'in_progress'),coalesce(u.source,''),u.earned_at,u.expires_at,coalesce(u.rule_version,0),e.title_id IS NOT NULL,coalesce(p.rule_version,0),p.counts,p.checked_at
	 FROM titles t LEFT JOIN user_titles u ON u.title_id=t.id AND u.user_id=$1 LEFT JOIN title_progress p ON p.title_id=t.id AND p.user_id=$1 LEFT JOIN title_equipment e ON e.title_id=t.id AND e.user_id=$1`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type record struct {
		u       UserTitle
		version int64
	}
	data := map[int64]record{}
	for rows.Next() {
		var id, v int64
		var b []byte
		var u UserTitle
		if err = rows.Scan(&id, &u.Status, &u.Source, &u.EarnedAt, &u.ExpiresAt, &u.EarnedVersion, &u.Equipped, &v, &b, &u.CheckedAt); err != nil {
			return nil, err
		}
		u.Counts = []int64{}
		if len(b) > 0 {
			if err = json.Unmarshal(b, &u.Counts); err != nil {
				return nil, err
			}
		}
		data[id] = record{u, v}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := []UserTitle{}
	now := time.Now()
	for _, c := range defs {
		r := data[c.ID]
		u := r.u
		if c.Status == "draft" {
			continue
		}
		u.Title = c
		if c.Mode == "manual" && u.Status == "in_progress" {
			u.Status = "not_earned"
		}
		u.ProgressPending = c.Mode == "automatic" && r.version != c.Version
		if u.ProgressPending {
			u.Counts = []int64{}
			u.CheckedAt = nil
		}
		if u.Status == "earned" && u.ExpiresAt != nil && !now.Before(*u.ExpiresAt) {
			u.Status = "expired"
		}
		if u.Status != "earned" || c.Status == "disabled" {
			u.Equipped = false
		}
		out = append(out, u)
	}
	return out, nil
}

func (s *Store) TitleJobs(ctx context.Context, tid int64, page int) ([]map[string]any, int, error) {
	if page < 1 {
		page = 1
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM title_jobs WHERE title_id=$1`, tid).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,rule_version,cursor_id,max_user_id,processed,awarded,status,created_at FROM title_jobs WHERE title_id=$1 ORDER BY id DESC LIMIT 30 OFFSET $2`, tid, (page-1)*30)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, ver, cursor, max, n, awards int64
		var status string
		var at time.Time
		if err = rows.Scan(&id, &ver, &cursor, &max, &n, &awards, &status, &at); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{"id": strconv.FormatInt(id, 10), "version": ver, "cursor": strconv.FormatInt(cursor, 10), "maxUserId": strconv.FormatInt(max, 10), "processed": n, "awarded": awards, "status": status, "createdAt": at})
	}
	return out, total, rows.Err()
}

func (s *Store) TitleLogs(ctx context.Context, tid int64, page int) ([]map[string]any, int, error) {
	if page < 1 {
		page = 1
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM title_logs WHERE title_id=$1`, tid).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,coalesce(user_id,0),coalesce(actor_id,0),action,detail,created_at FROM title_logs WHERE title_id=$1 ORDER BY id DESC LIMIT 30 OFFSET $2`, tid, (page-1)*30)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, uid, actor int64
		var action string
		var b json.RawMessage
		var at time.Time
		if err = rows.Scan(&id, &uid, &actor, &action, &b, &at); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{"id": strconv.FormatInt(id, 10), "userId": strconv.FormatInt(uid, 10), "actorId": strconv.FormatInt(actor, 10), "action": action, "detail": b, "createdAt": at})
	}
	return out, total, rows.Err()
}
