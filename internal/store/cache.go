// SPDX-License-Identifier: AGPL-3.0-or-later
// store/cache.go：进程内缓存（会话 30s 正负缓存、用户名缓存），省每请求一次 DB 往返。
package store

import (
	"context"
	"sync"
	"time"
)

// ---- 会话缓存：正缓存 30s，省每请求一次 DB 往返 ----

type sessionCache struct {
	store *Store
	mu    sync.RWMutex
	m     map[string]cacheEntry[*Session]
}

type cacheEntry[T any] struct {
	val T
	exp time.Time
}

func newSessionCache(s *Store) *sessionCache {
	c := &sessionCache{store: s, m: make(map[string]cacheEntry[*Session])}
	go c.janitor()
	return c
}

func (c *sessionCache) get(ctx context.Context, token string) (*Session, error) {
	c.mu.RLock()
	if e, ok := c.m[token]; ok && time.Now().Before(e.exp) {
		c.mu.RUnlock()
		if e.val == nil {
			return nil, ErrNotFound
		}
		return e.val, nil
	}
	c.mu.RUnlock()

	sess, err := c.store.Session(ctx, token)
	if err != nil {
		if err == ErrNotFound {
			c.set(token, nil, 15*time.Second) // 负缓存防穿透
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.set(token, sess, 30*time.Second)
	return sess, nil
}

func (c *sessionCache) set(token string, val *Session, ttl time.Duration) {
	c.mu.Lock()
	c.m[token] = cacheEntry[*Session]{val: val, exp: time.Now().Add(ttl)}
	c.mu.Unlock()
}

func (c *sessionCache) invalidate(token string) {
	c.mu.Lock()
	delete(c.m, token)
	c.mu.Unlock()
}

// invalidateUser 清掉该用户的全部会话缓存项（改密/重置后其他设备立即下线，
// 不等 30s 正缓存自然过期）。
func (c *sessionCache) invalidateUser(uid int64) {
	c.mu.Lock()
	for k, e := range c.m {
		if e.val != nil && e.val.UserID == uid {
			delete(c.m, k)
		}
	}
	c.mu.Unlock()
}

func (c *sessionCache) janitor() {
	for range time.Tick(time.Minute) {
		now := time.Now()
		c.mu.Lock()
		for k, e := range c.m {
			if now.After(e.exp) {
				delete(c.m, k)
			}
		}
		c.mu.Unlock()
	}
}

// ---- 用户名缓存（头像 SVG、楼层作者展示）----

type nameCache struct {
	store *Store
	mu    sync.RWMutex
	m     map[int64]cacheEntry[string]
}

func newNameCache(s *Store) *nameCache {
	return &nameCache{store: s, m: make(map[int64]cacheEntry[string])}
}

func (c *nameCache) name(ctx context.Context, uid int64) (string, error) {
	c.mu.RLock()
	if e, ok := c.m[uid]; ok && time.Now().Before(e.exp) {
		c.mu.RUnlock()
		return e.val, nil
	}
	c.mu.RUnlock()

	var name string
	err := c.store.pool.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, uid).Scan(&name)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.m[uid] = cacheEntry[string]{val: name, exp: time.Now().Add(5 * time.Minute)}
	c.mu.Unlock()
	return name, nil
}

func (c *nameCache) invalidate(uid int64) {
	c.mu.Lock()
	delete(c.m, uid)
	c.mu.Unlock()
}
