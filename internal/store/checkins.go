package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Checkin struct {
	UserID      int64     `json:"userId,string"`
	Day         string    `json:"day"`
	Streak      int       `json:"streak"`
	Experience  int64     `json:"experience"`
	Points      int64     `json:"points"`
	RuleVersion int64     `json:"ruleVersion"`
	TimeZone    string    `json:"timeZone"`
	CreatedAt   time.Time `json:"createdAt"`
}

const checkinSelect = `SELECT user_id,day::text,streak,experience,points,rule_version,time_zone,created_at FROM checkin_records`

func scanCheckin(row pgx.Row) (Checkin, error) {
	var c Checkin
	err := row.Scan(&c.UserID, &c.Day, &c.Streak, &c.Experience, &c.Points, &c.RuleVersion, &c.TimeZone, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

type CheckinStatus struct {
	Day       string   `json:"day"`
	TimeZone  string   `json:"timeZone"`
	Enabled   bool     `json:"enabled"`
	CheckedIn bool     `json:"checkedIn"`
	Streak    int      `json:"streak"`
	Checkin   *Checkin `json:"checkin"`
}

func (s *Store) CheckinStatus(ctx context.Context, uid int64) (CheckinStatus, error) {
	cfg, err := s.EngagementConfig(ctx)
	if err != nil {
		return CheckinStatus{}, err
	}
	v := CheckinStatus{TimeZone: cfg.Checkin.TimeZone, Enabled: cfg.Checkin.Enabled}
	if err = s.pool.QueryRow(ctx, `SELECT (clock_timestamp() AT TIME ZONE $1)::date::text`, v.TimeZone).Scan(&v.Day); err != nil {
		return v, err
	}
	c, err := scanCheckin(s.pool.QueryRow(ctx, checkinSelect+` WHERE user_id=$1 AND day>=$2::date-1 AND day<=$2::date ORDER BY day DESC LIMIT 1`, uid, v.Day))
	if errors.Is(err, ErrNotFound) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	v.Streak = c.Streak
	if c.Day == v.Day {
		v.CheckedIn = true
		v.Checkin = &c
	}
	return v, nil
}
func (s *Store) CheckinHistory(ctx context.Context, uid int64, before string) ([]Checkin, error) {
	if before != "" {
		if _, err := time.Parse("2006-01-02", before); err != nil {
			return nil, ErrEngagementInvalid
		}
	}
	rows, err := s.pool.Query(ctx, checkinSelect+` WHERE user_id=$1 AND (NULLIF($2,'')::date IS NULL OR day<NULLIF($2,'')::date) ORDER BY day DESC LIMIT 31`, uid, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Checkin{}
	for rows.Next() {
		c, err := scanCheckin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) ClaimCheckin(ctx context.Context, uid int64) (Checkin, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Checkin{}, err
	}
	defer tx.Rollback(ctx)
	cfg, err := engagementConfig(ctx, tx, true)
	if err != nil {
		return Checkin{}, err
	}
	mc, err := membershipConfig(ctx, tx, true)
	if err != nil {
		return Checkin{}, err
	}
	// Match growth-worker lock order: membership config, member state, points.
	var locked int64
	if err = tx.QueryRow(ctx, `SELECT user_id FROM member_states WHERE user_id=$1 FOR UPDATE`, uid).Scan(&locked); err != nil {
		return Checkin{}, err
	}
	if err = engagementActor(ctx, tx, uid); err != nil {
		return Checkin{}, err
	}
	var day string
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT (clock_timestamp() AT TIME ZONE $1)::date::text,clock_timestamp()`, cfg.Checkin.TimeZone).Scan(&day, &now); err != nil {
		return Checkin{}, err
	}
	old, err := scanCheckin(tx.QueryRow(ctx, checkinSelect+` WHERE user_id=$1 AND day=$2::date`, uid, day))
	if err == nil {
		return old, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Checkin{}, err
	}
	if !cfg.Checkin.Enabled {
		return Checkin{}, ErrEngagementClosed
	}
	streak := 1
	var previous int
	err = tx.QueryRow(ctx, `SELECT streak FROM checkin_records WHERE user_id=$1 AND day=$2::date-1`, uid, day).Scan(&previous)
	if err == nil {
		streak = previous + 1
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Checkin{}, err
	}
	c := Checkin{UserID: uid, Day: day, Streak: streak, Experience: cfg.Checkin.Experience, Points: cfg.Checkin.Points, RuleVersion: cfg.Version, TimeZone: cfg.Checkin.TimeZone, CreatedAt: now}
	if _, err = tx.Exec(ctx, `INSERT INTO checkin_records(user_id,day,streak,experience,points,rule_version,time_zone,created_at) VALUES($1,$2::date,$3,$4,$5,$6,$7,$8)`, uid, day, streak, c.Experience, c.Points, c.RuleVersion, c.TimeZone, now); err != nil {
		return Checkin{}, err
	}
	source := fmt.Sprintf("checkin:%s", day)
	if _, err = tx.Exec(ctx, `INSERT INTO member_experience(user_id,source,kind,delta,rule_version,reason,actor_id,reversible,created_at) VALUES($1,$2,'checkin',$3,$4,'每日签到奖励',$1,false,$5)`, uid, source, c.Experience, c.RuleVersion, now); err != nil {
		return Checkin{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE member_states SET experience=experience+$2,version=version+1 WHERE user_id=$1`, uid, c.Experience); err != nil {
		return Checkin{}, err
	}
	a, err := lockPoints(ctx, tx, uid)
	if err != nil {
		return Checkin{}, err
	}
	if err = appendPoints(ctx, tx, a, PointsEntry{Source: source, Kind: "checkin", Delta: c.Points, RuleVersion: c.RuleVersion, Reason: "每日签到奖励", ActorID: uid, EventAt: now}); err != nil {
		return Checkin{}, err
	}
	if err = upgradeMember(ctx, tx, mc, uid); err != nil {
		return Checkin{}, err
	}
	return c, tx.Commit(ctx)
}
