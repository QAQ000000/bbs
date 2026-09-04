// Package limiter 进程内固定窗口限流器：
// 以 (key, window) 为粒度计数，超出阈值拒绝。用于登录/注册/发帖/搜索/
// 上传/SSE 建连等入口的反滥用。单实例定位下与缓存同为进程内状态。
package limiter

import (
	"sync"
	"time"
)

type window struct {
	start time.Time
	count int
}

// Limiter 固定窗口计数器集合。
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*window
	stop    chan struct{}
	once    sync.Once
}

// New 创建限流器并启动过期清理。
func New() *Limiter {
	l := &Limiter{buckets: make(map[string]*window), stop: make(chan struct{})}
	go l.janitor()
	return l
}

// Allow 判定 key 在 win 窗口内是否还有配额（调用即计数）。
func (l *Limiter) Allow(key string, limit int, win time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	w := l.buckets[key]
	if w == nil || now.Sub(w.start) >= win {
		l.buckets[key] = &window{start: now, count: 1}
		return limit >= 1
	}
	w.count++
	return w.count <= limit
}

func (l *Limiter) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case now := <-t.C:
			l.mu.Lock()
			for k, w := range l.buckets {
				if now.Sub(w.start) > 2*time.Hour {
					delete(l.buckets, k)
				}
			}
			l.mu.Unlock()
		}
	}
}

// Stop 停止清理协程（进程退出无需调用，主要为测试提供收尾）。
func (l *Limiter) Stop() {
	l.once.Do(func() { close(l.stop) })
}
