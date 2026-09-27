// SPDX-License-Identifier: AGPL-3.0-or-later
// Package live 实现基于 SSE 的实时推送中枢：
// 订阅者按主题（"t:<tid>" 帖子、"f:<fid>" 版块）分组，
// 发帖/编辑等动作发生时由 API 层调用 Publish 广播数据事件。
package live

import (
	"sync"
)

// Event 是一次广播：Topic 决定推给谁，Payload 是已经 JSON 编码的事件体。
type Event struct {
	Topic   string
	Payload []byte
}

// Subscriber 代表一条 SSE 连接。
type Subscriber struct {
	ch     chan []byte
	topics map[string]struct{}
	closed bool
	mu     sync.Mutex
}

// C 返回事件通道（SSE handler 消费）。
func (s *Subscriber) C() <-chan []byte { return s.ch }

func (s *Subscriber) send(payload []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.ch <- payload:
		return true
	default:
		// 缓冲已满 = 慢消费者：关闭通道让 SSE handler 退出（客户端自动重连）。
		// 必须同时 close：只标记 closed 会让 handler 永远阻塞在心跳上，
		// 已投进缓冲的事件也不再被消费，连接既不断开也收不到后续事件。
		s.closed = true
		close(s.ch)
		return false
	}
}

func (s *Subscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

// Hub 管理全部订阅者。
type Hub struct {
	mu   sync.RWMutex
	subs map[*Subscriber]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[*Subscriber]struct{})}
}

// Subscribe 注册订阅者并绑定主题，返回取消函数。
func (h *Hub) Subscribe(buf int, topics ...string) (*Subscriber, func()) {
	sub := &Subscriber{
		ch:     make(chan []byte, buf),
		topics: make(map[string]struct{}, len(topics)),
	}
	for _, t := range topics {
		sub.topics[t] = struct{}{}
	}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		if _, ok := h.subs[sub]; ok {
			delete(h.subs, sub)
			sub.close()
		}
		h.mu.Unlock()
	}
	return sub, cancel
}

// Publish 向所有订阅了该主题的订阅者广播事件。
// 遍历时对已满缓冲的订阅者做非阻塞投递，绝不阻塞发帖路径。
func (h *Hub) Publish(e Event) {
	h.mu.RLock()
	targets := make([]*Subscriber, 0, len(h.subs))
	for sub := range h.subs {
		if _, ok := sub.topics[e.Topic]; ok {
			targets = append(targets, sub)
		}
	}
	h.mu.RUnlock()
	for _, sub := range targets {
		if !sub.send(e.Payload) {
			go func(s *Subscriber) {
				h.mu.Lock()
				if _, ok := h.subs[s]; ok {
					delete(h.subs, s)
					s.close()
				}
				h.mu.Unlock()
			}(sub)
		}
	}
}

// Count 返回当前订阅数（监控用）。
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// HasSubscribers is a point-in-time hint for optional live work, never a durable
// delivery decision. A client must refresh authoritative state after connecting.
func (h *Hub) HasSubscribers(topic string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.subs {
		if _, ok := sub.topics[topic]; ok {
			sub.mu.Lock()
			active := !sub.closed
			sub.mu.Unlock()
			if active {
				return true
			}
		}
	}
	return false
}
