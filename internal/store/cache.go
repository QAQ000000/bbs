// SPDX-License-Identifier: AGPL-3.0-or-later
// store/cache.go：用户名缓存。会话鉴权直接查询数据库以支持即时撤销。
package store

import (
	"context"
	"sync"
	"time"
)

type cacheEntry[T any] struct {
	val T
	exp time.Time
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
