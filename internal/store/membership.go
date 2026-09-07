// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

const memberDaySQL = `(now() AT TIME ZONE 'Asia/Shanghai')::date`

type MemberState struct {
	Restricted    bool         `json:"restricted"`
	UserID        int64        `json:"userId,string"`
	LevelID       int          `json:"levelId"`
	Experience    int64        `json:"experience"`
	Locked        bool         `json:"locked"`
	Version       int64        `json:"version"`
	Level         MemberLevel  `json:"level"`
	NextLevel     *MemberLevel `json:"nextLevel"`
	DaysVisited   int          `json:"daysVisited"`
	PostsRead     int64        `json:"postsRead"`
	PostCount     int64        `json:"postCount"`
	EmailVerified bool         `json:"emailVerified"`
}

func membershipConfig(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, lock bool) (MembershipConfig, error) {
	var c MembershipConfig
	var b []byte
	var v int64
	sql := `SELECT version,body FROM membership_config WHERE id`
	if lock {
		sql += ` FOR SHARE`
	}
	err := q.QueryRow(ctx, sql).Scan(&v, &b)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	c.Version = v
	c.Normalize()
	return c, err
}
func (s *Store) MembershipConfig(ctx context.Context) (MembershipConfig, error) {
	if c, ok := ctx.Value(membershipSnapshotKey{}).(MembershipConfig); ok {
		return c, nil
	}
	return membershipConfig(ctx, s.pool, false)
}

type membershipSnapshotKey struct{}

// WithMembershipSnapshot reuses an immutable configuration within one request.
// Transactional configuration reads always go directly to PostgreSQL.
func WithMembershipSnapshot(ctx context.Context, c MembershipConfig) context.Context {
	return context.WithValue(ctx, membershipSnapshotKey{}, c)
}

// FreshMembershipConfig deliberately bypasses a request snapshot, including on SSE revalidation.
func (s *Store) FreshMembershipConfig(ctx context.Context) (MembershipConfig, error) {
	return membershipConfig(ctx, s.pool, false)
}

const memberSelect = `SELECT ms.user_id,ms.level_id,ms.experience,ms.locked,ms.version,u.days_visited,u.posts_read,u.post_count,u.email_verified,(coalesce(u.banned_until>now(),false) OR coalesce(u.blocked_until>now(),false)) FROM member_states ms JOIN users u ON u.id=ms.user_id`

func scanMember(row pgx.Row, c MembershipConfig) (MemberState, error) {
	var m MemberState
	err := row.Scan(&m.UserID, &m.LevelID, &m.Experience, &m.Locked, &m.Version, &m.DaysVisited, &m.PostsRead, &m.PostCount, &m.EmailVerified, &m.Restricted)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	var ok bool
	m.Level, ok = c.Level(m.LevelID)
	if !ok {
		return m, fmt.Errorf("unknown member level %d", m.LevelID)
	}
	for _, l := range c.Levels {
		if l.Automatic && l.Rank > m.Level.Rank {
			v := l
			m.NextLevel = &v
			break
		}
	}
	return m, nil
}
func (s *Store) MembershipWithConfig(ctx context.Context, uid int64, c MembershipConfig) (MemberState, error) {
	return scanMember(s.pool.QueryRow(ctx, memberSelect+` WHERE ms.user_id=$1`, uid), c)
}

func (s *Store) Membership(ctx context.Context, uid int64) (MemberState, error) {
	c, err := s.MembershipConfig(ctx)
	if err != nil {
		return MemberState{}, err
	}
	return scanMember(s.pool.QueryRow(ctx, memberSelect+` WHERE ms.user_id=$1`, uid), c)
}
func (s *Store) MemberSummaries(ctx context.Context, ids []int64) (map[int64]LevelBadgeSummary, error) {
	out := map[int64]LevelBadgeSummary{}
	if len(ids) == 0 {
		return out, nil
	}
	c, err := s.MembershipConfig(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id,level_id FROM member_states WHERE user_id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		var lid int
		if err = rows.Scan(&uid, &lid); err != nil {
			return nil, err
		}
		l, ok := c.Level(lid)
		if !ok {
			return nil, ErrMembershipConflict
		}
		out[uid] = LevelBadgeSummary{l.ID, l.Name, l.Rank, l.Badge}
	}
	return out, rows.Err()
}

type LevelBadgeSummary struct {
	ID    int        `json:"id"`
	Name  string     `json:"name"`
	Rank  int        `json:"rank"`
	Badge LevelBadge `json:"badge"`
}

func targetLevel(c MembershipConfig, m MemberState) int {
	if m.Locked || m.Restricted {
		return m.LevelID
	}
	current, ok := c.Level(m.LevelID)
	if !ok {
		return m.LevelID
	}
	best := current
	for _, l := range c.Levels {
		if l.Automatic && l.Rank > best.Rank && m.Experience >= l.Experience && m.DaysVisited >= l.DaysVisited && m.PostsRead >= l.PostsRead && m.PostCount >= l.PostCount && (!l.EmailVerified || m.EmailVerified) {
			best = l
		}
	}
	return best.ID
}
func memberAudit(ctx context.Context, tx pgx.Tx, uid, actor int64, action string, detail any) error {
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO member_changes(user_id,actor_id,action,detail) VALUES(NULLIF($1,0),NULLIF($2,0),$3,$4)`, uid, actor, action, b)
	return err
}
func upgradeMember(ctx context.Context, tx pgx.Tx, c MembershipConfig, uid int64) error {
	m, err := scanMember(tx.QueryRow(ctx, memberSelect+` WHERE ms.user_id=$1 FOR UPDATE OF ms`, uid), c)
	if err != nil {
		return err
	}
	lid := targetLevel(c, m)
	if lid == m.LevelID {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE member_states SET level_id=$2,version=version+1 WHERE user_id=$1`, uid, lid); err != nil {
		return err
	}
	return memberAudit(ctx, tx, uid, 0, "level.upgrade", map[string]any{"from": m.LevelID, "to": lid, "configVersion": c.Version})
}

type growthEvent struct {
	ID, UID, Version int64
	Kind, Source     string
	Active           bool
	Rule             GrowthRule
	Created          time.Time
	PointsRule       *GrowthRule
	PointsVersion    int64
}

// ProcessMemberEvents drains a bounded durable queue. One transaction owns the
// queue so award/reversal order and daily gross caps remain deterministic.
func (s *Store) ProcessMemberEvents(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(7823491)`).Scan(&locked); err != nil || !locked {
		return 0, err
	}
	c, err := membershipConfig(ctx, tx, true)
	if err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id,user_id,kind,source,active,rule,rule_version,created_at,points_rule,coalesce(points_version,0) FROM member_events ORDER BY id LIMIT $1 FOR UPDATE`, limit)
	if err != nil {
		return 0, err
	}
	var events []growthEvent
	for rows.Next() {
		var e growthEvent
		var b []byte
		var pointsRaw []byte
		if err = rows.Scan(&e.ID, &e.UID, &e.Kind, &e.Source, &e.Active, &b, &e.Version, &e.Created, &pointsRaw, &e.PointsVersion); err != nil {
			rows.Close()
			return 0, err
		}
		if err = json.Unmarshal(b, &e.Rule); err != nil {
			rows.Close()
			return 0, err
		}
		events = append(events, e)
		if len(pointsRaw) > 0 {
			if err = json.Unmarshal(pointsRaw, &events[len(events)-1].PointsRule); err != nil {
				rows.Close()
				return 0, err
			}
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for _, e := range events {
		if err = applyGrowthEvent(ctx, tx, e); err != nil {
			return 0, err
		}
		if err = applyPointsEvent(ctx, tx, e); err != nil {
			return 0, err
		}
		if err = upgradeMember(ctx, tx, c, e.UID); err != nil {
			return 0, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM member_events WHERE id=$1`, e.ID); err != nil {
			return 0, err
		}
	}
	return len(events), tx.Commit(ctx)
}
func applyGrowthEvent(ctx context.Context, tx pgx.Tx, e growthEvent) error {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM member_states WHERE user_id=$1 FOR UPDATE`, e.UID); err != nil {
		return err
	}
	var old int64
	var reversed, reversible bool
	err := tx.QueryRow(ctx, `SELECT delta,reversed,reversible FROM member_experience WHERE user_id=$1 AND source=$2`, e.UID, e.Source).Scan(&old, &reversed, &reversible)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		if e.Active || reversed || !reversible || old == 0 {
			return nil
		}
		if _, err = tx.Exec(ctx, `UPDATE member_experience SET reversed=true WHERE user_id=$1 AND source=$2`, e.UID, e.Source); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO member_experience(user_id,source,kind,delta,rule_version,reason,reversible) VALUES($1,$2,$3,$4,$5,'业务撤销，冲回原奖励',false)`, e.UID, "reverse:"+e.Source, e.Kind, -old, e.Version); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE member_states SET experience=experience-$2,version=version+1 WHERE user_id=$1`, e.UID, old)
		return err
	}
	if !e.Active {
		return nil
	}
	points := e.Rule.Points
	if !e.Rule.Enabled {
		points = 0
	}
	// event date, not processing date; zero-point receipts prevent toggling to farm later.
	var used int64
	if err = tx.QueryRow(ctx, `SELECT coalesce(sum(delta),0) FROM member_experience WHERE user_id=$1 AND kind=$2 AND delta>0 AND (created_at AT TIME ZONE 'Asia/Shanghai')::date=($3::timestamptz AT TIME ZONE 'Asia/Shanghai')::date`, e.UID, e.Kind, e.Created).Scan(&used); err != nil {
		return err
	}
	if points > e.Rule.DailyCap-used {
		points = e.Rule.DailyCap - used
	}
	if points < 0 {
		points = 0
	}
	if _, err = tx.Exec(ctx, `INSERT INTO member_experience(user_id,source,kind,delta,rule_version,reason,reversible,created_at) VALUES($1,$2,$3,$4,$5,'有效业务事件奖励',$6,$7)`, e.UID, e.Source, e.Kind, points, e.Version, e.Rule.Reverse, e.Created); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE member_states SET experience=experience+$2,version=version+1 WHERE user_id=$1`, e.UID, points)
	return err
}

// RecordMemberActivity is an explicit write, never a GET/SSR side effect.
func (s *Store) RecordMemberActivity(ctx context.Context, uid int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var day string
	err = tx.QueryRow(ctx, `UPDATE users SET days_visited=days_visited+1,last_visit_date=`+memberDaySQL+` WHERE id=$1 AND NOT coalesce(banned_until>now(),false) AND NOT coalesce(blocked_until>now(),false) AND last_visit_date IS DISTINCT FROM `+memberDaySQL+` RETURNING last_visit_date::text`, uid).Scan(&day)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		if _, err = tx.Exec(ctx, `SELECT member_enqueue($1,'active',$2,true)`, uid, "active:"+day); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// RecordMemberRead counts distinct visible posts, not gaps between floor numbers.
func (s *Store) RecordMemberRead(ctx context.Context, uid, pid, tid int64, floor int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO member_read_posts(user_id,post_id) SELECT $1,p.id FROM posts p JOIN threads t ON t.id=p.thread_id WHERE p.id=$2 AND p.thread_id=$3 AND NOT p.deleted AND NOT p.pending AND NOT t.deleted AND NOT t.pending ON CONFLICT DO NOTHING`, uid, pid, tid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err = tx.Exec(ctx, `UPDATE users SET posts_read=posts_read+1 WHERE id=$1`, uid); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO thread_reads(user_id,thread_id,last_floor) SELECT $1,thread_id,floor FROM posts WHERE id=$3 AND thread_id=$2 AND NOT deleted ON CONFLICT(user_id,thread_id) DO UPDATE SET last_floor=GREATEST(thread_reads.last_floor,EXCLUDED.last_floor),updated_at=now()`, uid, tid, pid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) UpgradeMember(ctx context.Context, uid int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := membershipConfig(ctx, tx, true)
	if err != nil {
		return err
	}
	if err = upgradeMember(ctx, tx, c, uid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReserveMemberQuota serializes concurrent requests in PostgreSQL. A failed
// request refunds its reservation; crashed requests conservatively consume it.
func (s *Store) ReserveMemberQuota(ctx context.Context, uid int64, action string, amount, limit int64) (string, error) {
	if amount < 0 {
		return "", ErrMemberQuota
	}
	if limit < 0 {
		return "", nil
	}
	var day string
	err := s.pool.QueryRow(ctx, `INSERT INTO member_daily(user_id,day,action,amount) SELECT $1,`+memberDaySQL+`,$2,$3::bigint WHERE $3::bigint<=$4::bigint ON CONFLICT(user_id,day,action) DO UPDATE SET amount=member_daily.amount+EXCLUDED.amount WHERE member_daily.amount+EXCLUDED.amount<=$4 RETURNING day::text`, uid, action, amount, limit).Scan(&day)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrMemberQuota
	}
	return day, err
}
func (s *Store) RefundMemberQuota(ctx context.Context, uid int64, day, action string, amount int64) error {
	if day == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE member_daily SET amount=GREATEST(0,amount-$4) WHERE user_id=$1 AND day=$2::date AND action=$3`, uid, day, action, amount)
	return err
}
func (s *Store) MemberQuota(ctx context.Context, uid int64) (map[string]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT action,amount FROM member_daily WHERE user_id=$1 AND day=`+memberDaySQL, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]int64{}
	for rows.Next() {
		var k string
		var n int64
		if err = rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		m[k] = n
	}
	return m, rows.Err()
}

type MemberPreview struct {
	AffectedUsers int    `json:"affectedUsers"`
	Token         string `json:"token"`
	Users         int    `json:"users"`
	Upgrades      int    `json:"upgrades"`
	Locked        int    `json:"locked"`
	ConfigVersion int64  `json:"configVersion"`
}

func previewMembers(ctx context.Context, tx pgx.Tx, c MembershipConfig, lock bool) (MemberPreview, []MemberState, error) {
	oldConfig, err := membershipConfig(ctx, tx, false)
	if err != nil {
		return MemberPreview{}, nil, err
	}
	oldForums, _ := json.Marshal(oldConfig.Forums)
	newForums, _ := json.Marshal(c.Forums)
	q := memberSelect + ` ORDER BY ms.user_id`
	if lock {
		q += ` FOR UPDATE OF ms`
	}
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return MemberPreview{}, nil, err
	}
	var members []MemberState
	p := MemberPreview{ConfigVersion: c.Version}
	h := sha256.New()
	b, _ := json.Marshal(c)
	h.Write(b)
	for rows.Next() {
		m, e := scanMember(rows, c)
		if e != nil {
			rows.Close()
			return p, nil, e
		}
		members = append(members, m)
		b, _ = json.Marshal(m)
		h.Write(b)
		p.Users++
		oldLevel, _ := oldConfig.Level(m.LevelID)
		oldBytes, _ := json.Marshal(oldLevel)
		newBytes, _ := json.Marshal(m.Level)
		if string(oldBytes) != string(newBytes) || string(oldForums) != string(newForums) || targetLevel(c, m) != m.LevelID {
			p.AffectedUsers++
		}
		if m.Locked {
			p.Locked++
		}
		if targetLevel(c, m) != m.LevelID {
			p.Upgrades++
		}
	}
	rows.Close()
	p.Token = hex.EncodeToString(h.Sum(nil))
	return p, members, rows.Err()
}
func (s *Store) PreviewMembership(ctx context.Context, c MembershipConfig) (MemberPreview, error) {
	if err := c.Validate(); err != nil {
		return MemberPreview{}, err
	}
	c.Normalize()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return MemberPreview{}, err
	}
	defer tx.Rollback(ctx)
	old, err := membershipConfig(ctx, tx, false)
	if err != nil {
		return MemberPreview{}, err
	}
	if old.Version != c.Version {
		return MemberPreview{}, ErrMembershipConflict
	}
	p, _, err := previewMembers(ctx, tx, c, false)
	return p, err
}
func (s *Store) ApplyMembership(ctx context.Context, c MembershipConfig, token string, actor int64) error {
	if err := c.Validate(); err != nil {
		return err
	}
	c.Normalize()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var version int64
	if err = tx.QueryRow(ctx, `SELECT version FROM membership_config WHERE id FOR UPDATE`).Scan(&version); err != nil {
		return err
	}
	if version != c.Version {
		return ErrMembershipConflict
	}
	// Configuration application is a bounded maintenance transaction: freeze the
	// population and eligibility counters so the approved preview matches execution.
	if _, err = tx.Exec(ctx, `LOCK TABLE users, member_states IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	p, members, err := previewMembers(ctx, tx, c, true)
	if err != nil {
		return err
	}
	if token == "" || p.Token != token {
		return ErrMembershipConflict
	}
	for _, f := range c.Forums {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM forums WHERE id=$1)`, f.ForumID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errors.New("配置引用的版块不存在")
		}
	}
	c.Version++
	b, _ := json.Marshal(c)
	if _, err = tx.Exec(ctx, `UPDATE membership_config SET version=$1,body=$2 WHERE id`, c.Version, b); err != nil {
		return err
	}
	for _, m := range members {
		if err = upgradeMember(ctx, tx, c, m.UserID); err != nil {
			return err
		}
	}
	if err = memberAudit(ctx, tx, 0, actor, "config.update", map[string]any{"config": c, "preview": p}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type MemberAdjustment struct {
	Version int64  `json:"version"`
	LevelID *int   `json:"levelId"`
	Locked  *bool  `json:"locked"`
	Delta   int64  `json:"delta"`
	Reason  string `json:"reason"`
	Key     string `json:"key"`
}

func (s *Store) AdjustMember(ctx context.Context, uid, actor int64, a MemberAdjustment) error {
	if len(a.Reason) < 1 || len(a.Reason) > 500 || len(a.Key) < 8 || len(a.Key) > 100 || a.Delta < -1e9 || a.Delta > 1e9 {
		return errors.New("须提供调整原因、8–100 字符幂等键及有效分值")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, err := membershipConfig(ctx, tx, true)
	if err != nil {
		return err
	}
	m, err := scanMember(tx.QueryRow(ctx, memberSelect+` WHERE ms.user_id=$1 FOR UPDATE OF ms`, uid), c)
	if err != nil {
		return err
	}
	var prior []byte
	err = tx.QueryRow(ctx, `SELECT detail FROM member_changes WHERE user_id=$1 AND action='member.adjust' AND detail->>'key'=$2 ORDER BY id DESC LIMIT 1`, uid, a.Key).Scan(&prior)
	if err == nil {
		var saved struct {
			Adjustment MemberAdjustment `json:"adjustment"`
			Actor      int64            `json:"actor"`
		}
		if json.Unmarshal(prior, &saved) == nil {
			x, _ := json.Marshal(saved.Adjustment)
			y, _ := json.Marshal(a)
			if string(x) == string(y) && saved.Actor == actor {
				return nil
			}
		}
		return ErrMembershipConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if m.Version != a.Version {
		return ErrMembershipConflict
	}
	if a.LevelID != nil {
		if _, ok := c.Level(*a.LevelID); !ok {
			return errors.New("等级不存在")
		}
		m.LevelID = *a.LevelID
	}
	if a.Locked != nil {
		m.Locked = *a.Locked
	}
	if m.Experience+a.Delta < 0 {
		return errors.New("调整后经验不能为负")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO member_experience(user_id,source,kind,delta,rule_version,reason,actor_id,reversible) VALUES($1,$2,'manual',$3,$4,$5,$6,false)`, uid, "manual:"+a.Key, a.Delta, c.Version, a.Reason, actor); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE member_states SET level_id=$2,locked=$3,experience=experience+$4,version=version+1 WHERE user_id=$1`, uid, m.LevelID, m.Locked, a.Delta); err != nil {
		return err
	}
	if err = memberAudit(ctx, tx, uid, actor, "member.adjust", map[string]any{"key": a.Key, "actor": actor, "adjustment": a}); err != nil {
		return err
	}
	if a.LevelID == nil {
		if err = upgradeMember(ctx, tx, c, uid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type ExperienceEntry struct {
	ID          int64     `json:"id,string"`
	Source      string    `json:"source"`
	Kind        string    `json:"kind"`
	Delta       int64     `json:"delta"`
	Reversed    bool      `json:"reversed"`
	RuleVersion int64     `json:"ruleVersion"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Store) MemberExperience(ctx context.Context, uid int64, page int) ([]ExperienceEntry, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM member_experience WHERE user_id=$1`, uid).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,source,kind,delta,reversed,rule_version,reason,created_at FROM member_experience WHERE user_id=$1 ORDER BY id DESC LIMIT 30 OFFSET $2`, uid, (page-1)*30)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ExperienceEntry{}
	for rows.Next() {
		var e ExperienceEntry
		if err = rows.Scan(&e.ID, &e.Source, &e.Kind, &e.Delta, &e.Reversed, &e.RuleVersion, &e.Reason, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
func (s *Store) MemberAudit(ctx context.Context, page int) ([]map[string]any, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM member_changes`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,coalesce(user_id,0),coalesce(actor_id,0),action,detail,created_at FROM member_changes ORDER BY id DESC LIMIT 30 OFFSET $1`, (page-1)*30)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, uid, actor int64
		var action string
		var b []byte
		var at time.Time
		if err = rows.Scan(&id, &uid, &actor, &action, &b, &at); err != nil {
			return nil, 0, err
		}
		out = append(out, map[string]any{"id": strconv.FormatInt(id, 10), "userId": strconv.FormatInt(uid, 10), "actorId": strconv.FormatInt(actor, 10), "action": action, "detail": json.RawMessage(b), "createdAt": at})
	}
	return out, total, rows.Err()
}

func (s *Store) MemberBanned(ctx context.Context, uid int64) (bool, error) {
	var b bool
	err := s.pool.QueryRow(ctx, `SELECT coalesce(banned_until>now(),false) FROM users WHERE id=$1`, uid).Scan(&b)
	return b, err
}

type memberCommitKey struct{}

// WithMemberCommitMarker preserves a quota if a write commits but a later read fails.
func WithMemberCommitMarker(ctx context.Context, mark func()) context.Context {
	return context.WithValue(ctx, memberCommitKey{}, mark)
}
func markMemberCommit(ctx context.Context) {
	if mark, ok := ctx.Value(memberCommitKey{}).(func()); ok {
		mark()
	}
}
