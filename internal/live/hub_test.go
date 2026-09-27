// SPDX-License-Identifier: AGPL-3.0-or-later

package live

import (
	"sync"
	"testing"
	"time"
)

func TestHasSubscribers(t *testing.T) {
	h := NewHub()
	if h.HasSubscribers("u:1") {
		t.Fatal("empty hub")
	}
	_, first := h.Subscribe(1, "u:1", "t:2")
	_, second := h.Subscribe(1, "u:1")
	defer first()
	defer second()
	if !h.HasSubscribers("u:1") || !h.HasSubscribers("t:2") || h.HasSubscribers("u:2") {
		t.Fatal("topic matching")
	}
	first()
	if !h.HasSubscribers("u:1") || h.HasSubscribers("t:2") {
		t.Fatal("multiple connections")
	}
	h.Publish(Event{Topic: "u:1"})
	h.Publish(Event{Topic: "u:1"})
	if h.HasSubscribers("u:1") {
		t.Fatal("overflowed connection is not active")
	}
	second()
}

func TestHasSubscribersConcurrent(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_, cancel := h.Subscribe(1, "u:1")
				h.HasSubscribers("u:1")
				h.Publish(Event{Topic: "u:1"})
				cancel()
			}
		}()
	}
	wg.Wait()
	if h.HasSubscribers("u:1") {
		t.Fatal("canceled connections remain active")
	}
}

// 慢订阅者缓冲溢出：通道必须关闭（SSE handler 退出、客户端重连），
// 只标记 closed 不 close 会让连接既不断开也永久失去事件。
func TestOverflowClosesChannel(t *testing.T) {
	h := NewHub()
	sub, cancel := h.Subscribe(2, "x")
	defer cancel()

	h.Publish(Event{Topic: "x", Payload: []byte("a")})
	h.Publish(Event{Topic: "x", Payload: []byte("b")}) // 缓冲 2/2
	h.Publish(Event{Topic: "x", Payload: []byte("c")}) // 溢出 → 关闭

	done := make(chan struct{})
	var got int
	go func() {
		defer close(done)
		for range sub.C() {
			got++
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("溢出后通道未关闭，SSE handler 将永久阻塞在心跳上")
	}
	if got != 2 {
		t.Fatalf("应先消费已缓冲的 2 条事件: %d", got)
	}
	// 关闭后再发布不得 panic；溢出订阅者由 Publish 异步移出
	h.Publish(Event{Topic: "x", Payload: []byte("d")})
	deadline := time.Now().Add(2 * time.Second)
	for h.Count() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("溢出订阅者未移出 hub")
		}
		time.Sleep(time.Millisecond)
	}
	// 幂等 close：重复 cancel 不 panic
	cancel()
}
