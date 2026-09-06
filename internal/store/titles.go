// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// All title workers and administrative mutations share this transaction lock.
// Business writes only append invalidations and never acquire it.
func titleLock(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7823492)`)
	return err
}

func titleDefinitions(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) ([]TitleDefinition, error) {
	rows, err := q.Query(ctx, `SELECT id,version,body FROM titles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TitleDefinition{}
	for rows.Next() {
		var c TitleDefinition
		var b []byte
		var id, ver int64
		if err = rows.Scan(&id, &ver, &b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &c); err != nil {
			return nil, err
		}
		c.ID = id
		c.Version = ver
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out, rows.Err()
}

func (s *Store) Titles(ctx context.Context) ([]TitleDefinition, error) {
	return titleDefinitions(ctx, s.pool)
}

func titleByID(ctx context.Context, tx pgx.Tx, id int64) (TitleDefinition, error) {
	var c TitleDefinition
	var b []byte
	var ver int64
	err := tx.QueryRow(ctx, `SELECT version,body FROM titles WHERE id=$1`, id).Scan(&ver, &b)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	c.ID = id
	c.Version = ver
	return c, err
}

func titleAudit(ctx context.Context, tx pgx.Tx, tid, uid, actor int64, action string, detail any, key *string) error {
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO title_logs(title_id,user_id,actor_id,action,detail,request_key) VALUES($1,nullif($2,0),nullif($3,0),$4,$5,$6)`, tid, uid, actor, action, b, key)
	return err
}

func validateTitleForums(ctx context.Context, tx pgx.Tx, c TitleDefinition) error {
	for _, v := range c.Conditions {
		if v.ForumID > 0 {
			var found bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM forums WHERE id=$1)`, v.ForumID).Scan(&found); err != nil {
				return err
			}
			if !found {
				return ErrTitleInvalid
			}
		}
	}
	return nil
}

func (s *Store) SaveTitle(ctx context.Context, c TitleDefinition, actor int64) (TitleDefinition, error) {
	if err := c.Validate(); err != nil {
		return c, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer tx.Rollback(ctx)
	if err = titleLock(ctx, tx); err != nil {
		return c, err
	}
	if err = validateTitleForums(ctx, tx, c); err != nil {
		return c, err
	}
	if c.ID == 0 {
		if c.Version != 0 || c.Status != "draft" {
			return c, ErrTitleInvalid
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM titles`).Scan(&count); err != nil {
			return c, err
		}
		if count >= 200 {
			return c, ErrTitleInvalid
		}
		c.Version = 1
		b, _ := json.Marshal(c)
		err = tx.QueryRow(ctx, `INSERT INTO titles(body) VALUES($1) RETURNING id`, b).Scan(&c.ID)
	} else {
		old, e := titleByID(ctx, tx, c.ID)
		if e != nil {
			return c, e
		}
		if old.Version != c.Version {
			return c, ErrTitleConflict
		}
		// Published titles can be paused or disabled, but cannot become invisible drafts.
		if old.Status != "draft" && c.Status == "draft" {
			return c, ErrTitleInvalid
		}
		c.Version++
		b, _ := json.Marshal(c)
		_, err = tx.Exec(ctx, `UPDATE titles SET version=$2,body=$3 WHERE id=$1`, c.ID, c.Version, b)
	}
	if err != nil {
		return c, err
	}
	if _, err = tx.Exec(ctx, `UPDATE title_jobs SET status='superseded' WHERE title_id=$1 AND status='pending'`, c.ID); err != nil {
		return c, err
	}
	if c.Status == "active" && c.Mode == "automatic" {
		if err = enqueueTitleJob(ctx, tx, c); err != nil {
			return c, err
		}
	}
	if c.Status == "disabled" {
		if _, err = tx.Exec(ctx, `DELETE FROM title_equipment WHERE title_id=$1`, c.ID); err != nil {
			return c, err
		}
	}
	if err = titleAudit(ctx, tx, c.ID, 0, actor, "configure", c, nil); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}

func enqueueTitleJob(ctx context.Context, tx pgx.Tx, c TitleDefinition) error {
	_, err := tx.Exec(ctx, `INSERT INTO title_jobs(title_id,rule_version,max_user_id) SELECT $1,$2,coalesce(max(id),0) FROM users`, c.ID, c.Version)
	return err
}

// Query actual valid business objects, never capped experience receipts. Cache
// the result per forum during one user's evaluation; GET reads persisted progress.
func titleCounts(ctx context.Context, tx pgx.Tx, uid int64, c TitleDefinition, cache map[int64]map[string]int64) ([]int64, error) {
	out := make([]int64, 0, len(c.Conditions))
	for _, cond := range c.Conditions {
		m, ok := cache[cond.ForumID]
		if !ok {
			m = map[string]int64{}
			var threads, replies, likes, digests, accepted, maxLikes, xp, active, age, verified int64
			err := tx.QueryRow(ctx, `WITH valid AS (
			 SELECT p.id,p.floor,t.digest FROM posts p JOIN threads t ON t.id=p.thread_id
			 WHERE p.author_id=$1 AND NOT p.deleted AND NOT p.pending AND NOT t.deleted AND NOT t.pending AND ($2::bigint=0 OR t.forum_id=$2)
			), likes AS (SELECT v.id,count(a.uid) n FROM valid v LEFT JOIN post_actions a ON a.pid=v.id AND a.action=1 AND a.uid<>$1 GROUP BY v.id)
			SELECT (SELECT count(*) FROM valid WHERE floor=1),(SELECT count(*) FROM valid WHERE floor>1),
			 (SELECT coalesce(sum(n),0)::bigint FROM likes),(SELECT count(*) FROM valid WHERE floor=1 AND digest),
			 (SELECT count(*) FROM valid v JOIN accepted_replies a ON a.post_id=v.id WHERE v.floor>1),
			 (SELECT coalesce(max(n),0) FROM likes),ms.experience,u.days_visited,
			 greatest(0,floor(extract(epoch FROM (now()-u.created_at))/86400))::bigint,CASE WHEN u.email_verified THEN 1 ELSE 0 END
			 FROM users u JOIN member_states ms ON ms.user_id=u.id WHERE u.id=$1`, uid, cond.ForumID).Scan(&threads, &replies, &likes, &digests, &accepted, &maxLikes, &xp, &active, &age, &verified)
			if err != nil {
				return nil, err
			}
			m["threads_created"] = threads
			m["replies_created"] = replies
			m["likes_received"] = likes
			m["featured_threads"] = digests
			m["accepted_replies"] = accepted
			m["post_likes_max"] = maxLikes
			m["experience"] = xp
			m["active_days"] = active
			m["registered_days"] = age
			m["email_verified"] = verified
			cache[cond.ForumID] = m
		}
		out = append(out, m[cond.Metric])
	}
	return out, nil
}

type TitlePreview struct {
	Eligible  int   `json:"eligible"`
	NewAwards int   `json:"newAwards"`
	Version   int64 `json:"version"`
}

func (s *Store) PreviewTitle(ctx context.Context, c TitleDefinition) (TitlePreview, error) {
	p := TitlePreview{Version: c.Version}
	if err := c.Validate(); err != nil {
		return p, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)
	if err = validateTitleForums(ctx, tx, c); err != nil {
		return p, err
	}
	if c.ID > 0 {
		old, e := titleByID(ctx, tx, c.ID)
		if e != nil {
			return p, e
		}
		if old.Version != c.Version {
			return p, ErrTitleConflict
		}
	}
	if c.Mode != "automatic" {
		return p, nil
	}
	var cursor int64
	for {
		ids, e := titleUserBatch(ctx, tx, cursor, 0, 100)
		if e != nil {
			return p, e
		}
		if len(ids) == 0 {
			break
		}
		for _, uid := range ids {
			counts, e := titleCounts(ctx, tx, uid, c, map[int64]map[string]int64{})
			if e != nil {
				return p, e
			}
			var restricted, owned bool
			if e = tx.QueryRow(ctx, `SELECT coalesce(banned_until>now(),false) OR coalesce(blocked_until>now(),false),EXISTS(SELECT 1 FROM user_titles WHERE user_id=$1 AND title_id=$2) FROM users WHERE id=$1`, uid, c.ID).Scan(&restricted, &owned); e != nil {
				return p, e
			}
			if !restricted && c.matches(counts) {
				p.Eligible++
				if !owned && c.issuing(time.Now()) {
					p.NewAwards++
				}
			}
			cursor = uid
		}
	}
	return p, nil
}

func titleUserBatch(ctx context.Context, tx pgx.Tx, cursor, max int64, limit int) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE id>$1 AND ($2::bigint=0 OR id<=$2) ORDER BY id LIMIT $3`, cursor, max, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func evaluateTitle(ctx context.Context, tx pgx.Tx, uid int64, c TitleDefinition, cache map[int64]map[string]int64) (bool, error) {
	counts, err := titleCounts(ctx, tx, uid, c, cache)
	if err != nil {
		return false, err
	}
	b, _ := json.Marshal(counts)
	if _, err = tx.Exec(ctx, `INSERT INTO title_progress(user_id,title_id,rule_version,counts) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,title_id) DO UPDATE SET rule_version=excluded.rule_version,counts=excluded.counts,checked_at=now()`, uid, c.ID, c.Version, b); err != nil {
		return false, err
	}
	if !c.issuing(time.Now()) || c.Mode != "automatic" || !c.matches(counts) {
		return false, nil
	}
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT NOT coalesce(banned_until>now(),false) AND NOT coalesce(blocked_until>now(),false) FROM users WHERE id=$1`, uid).Scan(&allowed); err != nil {
		return false, err
	}
	if !allowed {
		return false, nil
	}
	tag, err := tx.Exec(ctx, `INSERT INTO user_titles(user_id,title_id,status,source,rule_version,expires_at) VALUES($1,$2,'earned','automatic',$3,$4) ON CONFLICT DO NOTHING`, uid, c.ID, c.Version, c.expiry(time.Now()))
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	err = titleAudit(ctx, tx, c.ID, uid, 0, "grant", map[string]any{"version": c.Version, "counts": counts, "conditions": c.Conditions}, nil)
	return true, err
}

// Each batch is durable and bounded. Failed batches roll back, retaining their
// events/cursor; a separate queue avoids consuming membership's events twice.
func (s *Store) ProcessTitleWork(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(7823492)`).Scan(&locked); err != nil || !locked {
		return 0, err
	}
	defs, err := titleDefinitions(ctx, tx)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM title_equipment e USING user_titles u,titles t WHERE e.user_id=u.user_id AND e.title_id=u.title_id AND t.id=e.title_id AND (u.status<>'earned' OR u.expires_at<=now() OR t.body->>'status' NOT IN ('active','paused'))`); err != nil {
		return 0, err
	}
	// Hourly reconciliation covers age, scheduled opening, bans expiring and missed invalidations.
	var due bool
	if err = tx.QueryRow(ctx, `SELECT next_run<=now() FROM title_schedule WHERE id`).Scan(&due); err != nil {
		return 0, err
	}
	if due {
		for _, c := range defs {
			if c.Status == "active" && c.Mode == "automatic" {
				var pending bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM title_jobs WHERE title_id=$1 AND status='pending')`, c.ID).Scan(&pending); err != nil {
					return 0, err
				}
				if !pending {
					if err = enqueueTitleJob(ctx, tx, c); err != nil {
						return 0, err
					}
				}
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE title_schedule SET next_run=now()+interval '1 hour' WHERE id`); err != nil {
			return 0, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT id,user_id FROM title_events ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	users := map[int64]bool{}
	for rows.Next() {
		var id, uid int64
		if err = rows.Scan(&id, &uid); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
		users[uid] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for uid := range users {
		cache := map[int64]map[string]int64{}
		for _, c := range defs {
			if c.Status == "active" && c.Mode == "automatic" {
				if _, err = evaluateTitle(ctx, tx, uid, c, cache); err != nil {
					return 0, err
				}
			}
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM title_events WHERE id=ANY($1)`, ids); err != nil {
		return 0, err
	}
	var jid, tid, ver, cursor, max int64
	err = tx.QueryRow(ctx, `SELECT id,title_id,rule_version,cursor_id,max_user_id FROM title_jobs WHERE status='pending' ORDER BY id LIMIT 1`).Scan(&jid, &tid, &ver, &cursor, &max)
	n := len(ids)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if err == nil {
		c, e := titleByID(ctx, tx, tid)
		if e != nil {
			return 0, e
		}
		if c.Version != ver || c.Status != "active" || c.Mode != "automatic" {
			_, err = tx.Exec(ctx, `UPDATE title_jobs SET status='superseded' WHERE id=$1`, jid)
		} else {
			batch := []int64{}
			if max > 0 {
				batch, err = titleUserBatch(ctx, tx, cursor, max, limit)
				if err != nil {
					return 0, err
				}
			}
			awards := 0
			for _, uid := range batch {
				awarded, e := evaluateTitle(ctx, tx, uid, c, map[int64]map[string]int64{})
				if e != nil {
					return 0, e
				}
				if awarded {
					awards++
				}
				cursor = uid
			}
			status := "pending"
			if len(batch) < limit || cursor >= max {
				status = "complete"
			}
			_, err = tx.Exec(ctx, `UPDATE title_jobs SET cursor_id=$2,processed=processed+$3,awarded=awarded+$4,status=$5 WHERE id=$1`, jid, cursor, len(batch), awards, status)
			n += len(batch)
		}
		if err != nil {
			return 0, err
		}
	}
	return n, tx.Commit(ctx)
}

type TitleAdjustment struct {
	Action  string `json:"action"`
	Reason  string `json:"reason"`
	Key     string `json:"key"`
	Version int64  `json:"version"`
}

func (s *Store) AdjustTitle(ctx context.Context, uid, tid, actor int64, a TitleAdjustment) error {
	if (a.Action != "grant" && a.Action != "revoke") || strings.TrimSpace(a.Reason) == "" || len(a.Reason) > 500 || len(a.Key) < 8 || len(a.Key) > 100 {
		return ErrTitleInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = titleLock(ctx, tx); err != nil {
		return err
	}
	detail := map[string]any{"request": a, "userId": uid, "titleId": tid}
	b, _ := json.Marshal(detail)
	var same bool
	err = tx.QueryRow(ctx, `SELECT detail=$3::jsonb FROM title_logs WHERE actor_id=$1 AND request_key=$2`, actor, a.Key, b).Scan(&same)
	if err == nil {
		if same {
			return nil
		}
		return ErrTitleConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	c, err := titleByID(ctx, tx, tid)
	if err != nil {
		return err
	}
	if a.Version != c.Version {
		return ErrTitleConflict
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, uid).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if a.Action == "grant" {
		if !c.issuing(time.Now()) {
			return ErrTitleInvalid
		}
		_, err = tx.Exec(ctx, `INSERT INTO user_titles(user_id,title_id,status,source,rule_version,expires_at) VALUES($1,$2,'earned','manual',$3,$4) ON CONFLICT(user_id,title_id) DO UPDATE SET status='earned',source='manual',rule_version=excluded.rule_version,earned_at=now(),expires_at=excluded.expires_at`, uid, tid, c.Version, c.expiry(time.Now()))
	} else {
		// A revocation tombstone also prevents future automatic grants.
		_, err = tx.Exec(ctx, `INSERT INTO user_titles(user_id,title_id,status,source,rule_version) VALUES($1,$2,'revoked','manual',$3) ON CONFLICT(user_id,title_id) DO UPDATE SET status='revoked'`, uid, tid, c.Version)
		if err == nil {
			_, err = tx.Exec(ctx, `DELETE FROM title_equipment WHERE user_id=$1 AND title_id=$2`, uid, tid)
		}
	}
	if err != nil {
		return err
	}
	if err = titleAudit(ctx, tx, tid, uid, actor, a.Action, detail, &a.Key); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) EquipTitle(ctx context.Context, uid, tid int64) error {
	if tid < 0 {
		return ErrTitleInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = titleLock(ctx, tx); err != nil {
		return err
	}
	if tid == 0 {
		_, err = tx.Exec(ctx, `DELETE FROM title_equipment WHERE user_id=$1`, uid)
	} else {
		var eligible bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_titles u JOIN titles t ON t.id=u.title_id WHERE u.user_id=$1 AND u.title_id=$2 AND u.status='earned' AND (u.expires_at IS NULL OR u.expires_at>now()) AND t.body->>'status' IN ('active','paused'))`, uid, tid).Scan(&eligible)
		if err != nil {
			return err
		}
		if !eligible {
			return ErrTitleForbidden
		}
		_, err = tx.Exec(ctx, `INSERT INTO title_equipment(user_id,title_id) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET title_id=excluded.title_id`, uid, tid)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
