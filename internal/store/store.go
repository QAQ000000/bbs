// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// ErrNotFound 统一的“没有这条记录”。
var ErrNotFound = errors.New("not found")

// ErrUserHasContent 用户仍有公开内容，不能直接删号。
var ErrUserHasContent = errors.New("该用户仍有未删除的发帖，不能直接删号，请先处理其内容或使用禁言")

// ErrWrongPassword 密码校验失败。
var ErrWrongPassword = errors.New("wrong password")

// ErrEmailTaken 邮箱已被其他账号使用。
var ErrEmailTaken = errors.New("该邮箱已被其他账号使用")

// Store 封装全部数据库访问。
type Store struct {
	pool     *pgxpool.Pool
	sessions *sessionCache
	avatars  *nameCache

	viewCounterOnce sync.Once
	views           *viewCounter
}

func New(pool *pgxpool.Pool) *Store {
	s := &Store{pool: pool}
	s.sessions = newSessionCache(s)
	s.avatars = newNameCache(s)
	return s
}

// ---- 用户 ----

const userCols = `id, username, password_hash, email, group_id, post_count, signature, created_at,
	trust_level, posts_read, days_visited`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Email, &u.GroupID,
		&u.PostCount, &u.Signature, &u.CreatedAt, &u.TrustLevel, &u.PostsRead, &u.DaysVisited)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

// CreateUser 创建用户；用户名或邮箱冲突由唯一索引兜底。
func (s *Store) CreateUser(ctx context.Context, username, password, email string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash, email) VALUES ($1,$2,$3)
		 RETURNING `+userCols, username, string(hash), email)
	u, err := scanUser(row)
	if err != nil {
		return nil, err
	}
	s.avatars.invalidate(u.ID)
	return u, nil
}

func (s *Store) UserByName(ctx context.Context, username string) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE lower(username)=lower($1)`, username))
}

// UsersByNames 按用户名精确批量查询（提及解析用）。
func (s *Store) UsersByNames(ctx context.Context, names []string) ([]*User, error) {
	if len(names) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+userCols+` FROM users WHERE lower(username) = ANY($1)`, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id=$1`, id))
}

// VerifyPassword 校验密码。
func (s *Store) VerifyPassword(u *User, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}

// TouchLogin 记录最后登录时间。
func (s *Store) TouchLogin(ctx context.Context, uid int64) {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, uid)
	_ = err
}

// NameByID 用户名缓存查询（头像/楼层作者等高频展示用）。
func (s *Store) NameByID(ctx context.Context, uid int64) (string, error) {
	return s.avatars.name(ctx, uid)
}

// ---- 会话 ----

const sessionTTL = 30 * 24 * time.Hour

func newToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// CreateSession 建立会话，返回原始 token（仅出现在 Cookie 中）与 csrf token。
// 库中只保存 token 的 SHA-256：库泄露不直接等同会话接管。
func (s *Store) CreateSession(ctx context.Context, uid int64) (token, csrf string, err error) {
	token = newToken(32)
	csrf = newToken(24)
	_, err = s.pool.Exec(ctx,
		`INSERT INTO sessions (token, user_id, csrf, expires_at) VALUES ($1,$2,$3,now()+$4::interval)`,
		hashToken(token), uid, csrf, sessionTTL.String())
	return
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) Session(ctx context.Context, token string) (*Session, error) {
	var sess Session
	err := s.pool.QueryRow(ctx,
		`SELECT token, user_id, csrf, expires_at FROM sessions WHERE token=$1 AND expires_at > now()`,
		hashToken(token)).Scan(&sess.Token, &sess.UserID, &sess.CSRF, &sess.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &sess, err
}

// SessionCached 走 30 秒缓存的会话查询，省掉每请求一次 DB 往返。
func (s *Store) SessionCached(ctx context.Context, token string) (*Session, error) {
	return s.sessions.get(ctx, token)
}

func (s *Store) DeleteSession(ctx context.Context, token string) {
	s.sessions.invalidate(token)
	_, _ = s.pool.Exec(ctx, `DELETE FROM sessions WHERE token=$1`, hashToken(token))
}

func (s *Store) PurgeSessions(ctx context.Context) {
	_, _ = s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
}

// ---- 站点统计 ----

func (s *Store) SiteStats(ctx context.Context) (SiteStats, error) {
	var st SiteStats
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM posts WHERE created_at >= current_date),
		  (SELECT count(*) FROM posts WHERE created_at >= current_date - 1 AND created_at < current_date),
		  (SELECT count(*) FROM posts),
		  (SELECT count(*) FROM threads WHERE NOT deleted),
		  (SELECT count(*) FROM users)`).
		Scan(&st.TodayPosts, &st.Yesterday, &st.TotalPosts, &st.TotalThreads, &st.Members)
	return st, err
}

// ---- 批量浏览计数器：内存累计，2 秒合并落库一次 ----

type viewCounter struct {
	mu     sync.Mutex
	dirty  map[int64]int64
	stopCh chan struct{}
}

func (s *Store) StartViewCounter(ctx context.Context) {
	s.viewCounterOnce.Do(func() {
		vc := &viewCounter{dirty: make(map[int64]int64), stopCh: make(chan struct{})}
		s.views = vc
		go vc.flusher(s.pool, vc.stopCh)
	})
}

func (s *Store) StopViewCounter() {
	if s.views != nil {
		close(s.views.stopCh)
	}
}

func (vc *viewCounter) flusher(pool *pgxpool.Pool, stop <-chan struct{}) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			vc.mu.Lock()
			if len(vc.dirty) == 0 {
				vc.mu.Unlock()
				continue
			}
			batch := vc.dirty
			vc.dirty = make(map[int64]int64)
			vc.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			for tid, n := range batch {
				_, _ = pool.Exec(ctx, `UPDATE threads SET view_count = view_count + $1 WHERE id=$2`, n, tid)
			}
			cancel()
		}
	}
}

// IncView 累计一次浏览（异步合并落库）。
func (s *Store) IncView(tid int64) {
	if s.views == nil {
		return
	}
	s.views.mu.Lock()
	s.views.dirty[tid]++
	s.views.mu.Unlock()
}
