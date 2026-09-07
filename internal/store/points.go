package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var (
	ErrPointsInvalid      = errors.New("invalid points request")
	ErrPointsConflict     = errors.New("points version or idempotency conflict")
	ErrPointsInsufficient = errors.New("insufficient available points")
)
var PointsKinds = []string{"active", "thread", "reply", "like", "digest", "accepted"}

type PointsConfig struct {
	Version            int64                 `json:"version"`
	Rules              map[string]GrowthRule `json:"rules"`
	AcceptedExperience GrowthRule            `json:"acceptedExperience"`
}

func DefaultPointsConfig() PointsConfig {
	return PointsConfig{Version: 1, Rules: map[string]GrowthRule{
		"active": {false, 0, 0, false}, "thread": {true, 1, 10, true}, "reply": {true, 1, 20, true},
		"like": {true, 1, 20, true}, "digest": {true, 5, 25, true}, "accepted": {true, 10, 50, true},
	}, AcceptedExperience: GrowthRule{true, 30, 150, true}}
}

func (c PointsConfig) Validate() error {
	if c.Version < 1 || len(c.Rules) != len(PointsKinds) {
		return ErrPointsInvalid
	}
	check := func(r GrowthRule) bool {
		return r.Points >= 0 && r.Points <= 10000 && r.DailyCap >= 0 && r.DailyCap <= 1000000
	}
	for _, k := range PointsKinds {
		r, ok := c.Rules[k]
		if !ok || !check(r) {
			return ErrPointsInvalid
		}
	}
	if !check(c.AcceptedExperience) {
		return ErrPointsInvalid
	}
	return nil
}

func (s *Store) PointsConfig(ctx context.Context) (PointsConfig, error) {
	var c PointsConfig
	var raw []byte
	var version int64
	err := s.pool.QueryRow(ctx, `SELECT version,body FROM points_config WHERE id`).Scan(&version, &raw)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(raw, &c)
	c.Version = version
	return c, err
}

func (s *Store) SavePointsConfig(ctx context.Context, c PointsConfig, actor int64) error {
	if err := c.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE points_config SET version=version+1,body=$2 WHERE id AND version=$1`, c.Version, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrPointsConflict
	}
	if err = pointsAudit(ctx, tx, actor, "points.configure", fmt.Sprintf("version=%d", c.Version+1)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type PointsAccount struct {
	UserID    int64 `json:"userId,string"`
	Balance   int64 `json:"balance"`
	Frozen    int64 `json:"frozen"`
	Available int64 `json:"available"`
	Debt      int64 `json:"debt"`
	Version   int64 `json:"version"`
}

func (s *Store) PointsAccount(ctx context.Context, uid int64) (PointsAccount, error) {
	var a PointsAccount
	err := s.pool.QueryRow(ctx, `SELECT u.id,coalesce(p.balance,0),coalesce(p.frozen,0),coalesce(p.version,1) FROM users u LEFT JOIN points_accounts p ON p.user_id=u.id WHERE u.id=$1`, uid).Scan(&a.UserID, &a.Balance, &a.Frozen, &a.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	a.Available = max(a.Balance-a.Frozen, 0)
	a.Debt = max(a.Frozen-a.Balance, 0)
	return a, err
}

type PointsEntry struct {
	ID           int64     `json:"id,string"`
	UserID       int64     `json:"userId,string"`
	Source       string    `json:"source"`
	Kind         string    `json:"kind"`
	Delta        int64     `json:"delta"`
	FrozenDelta  int64     `json:"frozenDelta"`
	BalanceAfter int64     `json:"balanceAfter"`
	FrozenAfter  int64     `json:"frozenAfter"`
	RuleVersion  int64     `json:"ruleVersion"`
	Reversible   bool      `json:"reversible"`
	Reason       string    `json:"reason"`
	ActorID      int64     `json:"actorId,string"`
	EventAt      time.Time `json:"eventAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (s *Store) PointsLedger(ctx context.Context, uid, before int64) ([]PointsEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,user_id,source,kind,delta,frozen_delta,balance_after,frozen_after,rule_version,reversible,reason,actor_id,event_at,created_at
 FROM points_ledger WHERE user_id=$1 AND kind<>'baseline' AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT 51`, uid, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PointsEntry{}
	for rows.Next() {
		var v PointsEntry
		if err = rows.Scan(&v.ID, &v.UserID, &v.Source, &v.Kind, &v.Delta, &v.FrozenDelta, &v.BalanceAfter, &v.FrozenAfter, &v.RuleVersion, &v.Reversible, &v.Reason, &v.ActorID, &v.EventAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func lockPoints(ctx context.Context, tx pgx.Tx, uid int64) (PointsAccount, error) {
	var a PointsAccount
	a.UserID = uid
	if _, err := tx.Exec(ctx, `INSERT INTO points_accounts(user_id) SELECT id FROM users WHERE id=$1 ON CONFLICT DO NOTHING`, uid); err != nil {
		return a, err
	}
	err := tx.QueryRow(ctx, `SELECT balance,frozen,version FROM points_accounts WHERE user_id=$1 FOR UPDATE`, uid).Scan(&a.Balance, &a.Frozen, &a.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// Every balance change and its immutable entry share one transaction and account lock.
func appendPoints(ctx context.Context, tx pgx.Tx, a PointsAccount, v PointsEntry) error {
	if _, err := tx.Exec(ctx, `UPDATE points_accounts SET balance=balance+$2,frozen=frozen+$3,version=version+1 WHERE user_id=$1`, a.UserID, v.Delta, v.FrozenDelta); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO points_ledger(user_id,source,kind,delta,frozen_delta,balance_after,frozen_after,rule_version,reversible,reason,actor_id,event_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, a.UserID, v.Source, v.Kind, v.Delta, v.FrozenDelta, a.Balance+v.Delta, a.Frozen+v.FrozenDelta, v.RuleVersion, v.Reversible, v.Reason, v.ActorID, v.EventAt)
	return err
}

func applyPointsEvent(ctx context.Context, tx pgx.Tx, e growthEvent) error {
	if e.PointsRule == nil {
		return nil
	} // Events queued before schema 13 do not grant points.
	a, err := lockPoints(ctx, tx, e.UID)
	if err != nil {
		return err
	}
	var old int64
	var reversible bool
	var originalVersion int64
	err = tx.QueryRow(ctx, `SELECT delta,reversible,rule_version FROM points_ledger WHERE user_id=$1 AND source=$2`, e.UID, e.Source).Scan(&old, &reversible, &originalVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		if e.Active || !reversible || old <= 0 {
			return nil
		}
		var reversed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM points_ledger WHERE user_id=$1 AND source=$2)`, e.UID, "reverse:"+e.Source).Scan(&reversed); err != nil {
			return err
		}
		if reversed {
			return nil
		}
		return appendPoints(ctx, tx, a, PointsEntry{Source: "reverse:" + e.Source, Kind: e.Kind, Delta: -old, RuleVersion: originalVersion, Reason: "业务撤销，冲回原积分奖励", EventAt: e.Created})
	}
	if !e.Active {
		return nil
	}
	rule := *e.PointsRule
	amount := rule.Points
	if !rule.Enabled {
		amount = 0
	}
	var used int64
	if err = tx.QueryRow(ctx, `SELECT coalesce(sum(delta),0) FROM points_ledger WHERE user_id=$1 AND kind=$2 AND delta>0 AND (event_at AT TIME ZONE 'Asia/Shanghai')::date=($3::timestamptz AT TIME ZONE 'Asia/Shanghai')::date`, e.UID, e.Kind, e.Created).Scan(&used); err != nil {
		return err
	}
	amount = max(0, min(amount, rule.DailyCap-used))
	return appendPoints(ctx, tx, a, PointsEntry{Source: e.Source, Kind: e.Kind, Delta: amount, RuleVersion: e.PointsVersion, Reversible: rule.Reverse, Reason: "有效业务事件积分奖励", EventAt: e.Created})
}

type PointsAdjustment struct {
	Version int64  `json:"version"`
	Delta   int64  `json:"delta"`
	Reason  string `json:"reason"`
	Key     string `json:"key"`
}

func (s *Store) AdjustPoints(ctx context.Context, uid, actor int64, v PointsAdjustment) error {
	if v.Version < 1 || v.Delta == 0 || v.Delta > 1000000 || v.Delta < -1000000 || !utf8.ValidString(v.Reason) || strings.TrimSpace(v.Reason) == "" || utf8.RuneCountInString(v.Reason) > 200 || strings.ContainsFunc(v.Reason, unicode.IsControl) || len(v.Key) < 8 || len(v.Key) > 80 {
		return ErrPointsInvalid
	}
	for _, ch := range v.Key {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return ErrPointsInvalid
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockPoints(ctx, tx, uid)
	if err != nil {
		return err
	}
	source := fmt.Sprintf("admin:%d:%s", actor, v.Key)
	var oldDelta int64
	var reason string
	err = tx.QueryRow(ctx, `SELECT delta,reason FROM points_ledger WHERE user_id=$1 AND source=$2`, uid, source).Scan(&oldDelta, &reason)
	if err == nil {
		if oldDelta != v.Delta || reason != v.Reason {
			return ErrPointsConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if v.Version != a.Version {
		return ErrPointsConflict
	}
	if v.Delta < 0 && -v.Delta > max(a.Balance-a.Frozen, 0) {
		return ErrPointsInsufficient
	}
	if a.Balance+v.Delta > 9000000000000 {
		return ErrPointsInvalid
	}
	if err = appendPoints(ctx, tx, a, PointsEntry{Source: source, Kind: "admin", Delta: v.Delta, Reason: v.Reason, ActorID: actor, EventAt: time.Now()}); err != nil {
		return err
	}
	if err = pointsAudit(ctx, tx, actor, "points.adjust", fmt.Sprintf("user=%d delta=%d key=%s reason=%s", uid, v.Delta, v.Key, v.Reason)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func pointsAudit(ctx context.Context, tx pgx.Tx, actor int64, action, detail string) error {
	tag, err := tx.Exec(ctx, `INSERT INTO admin_logs(uid,username,action,detail) SELECT id,username,$2,$3 FROM users WHERE id=$1`, actor, action, detail)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}

// ReconcilePoints is read-only; accounting history is never rewritten to hide a mismatch.
func (s *Store) ReconcilePoints(ctx context.Context, uid int64) (map[string]any, error) {
	var balance, frozen, ledgerBalance, ledgerFrozen int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce(a.balance,0),coalesce(a.frozen,0),coalesce((SELECT sum(delta) FROM points_ledger WHERE user_id=u.id),0),coalesce((SELECT sum(frozen_delta) FROM points_ledger WHERE user_id=u.id),0)
 FROM users u LEFT JOIN points_accounts a ON a.user_id=u.id WHERE u.id=$1`, uid).Scan(&balance, &frozen, &ledgerBalance, &ledgerFrozen)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return map[string]any{"userId": fmt.Sprint(uid), "balance": balance, "frozen": frozen, "ledgerBalance": ledgerBalance, "ledgerFrozen": ledgerFrozen, "consistent": balance == ledgerBalance && frozen == ledgerFrozen}, err
}
