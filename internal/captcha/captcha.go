// SPDX-License-Identifier: AGPL-3.0-or-later
// Package captcha 零依赖算术验证码（ROADMAP 阶段四）：
// SVG 输出，答案进程内存储、一次性消费、10 分钟过期——与单实例定位一致。
package captcha

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html"
	"math/big"
	"strings"
	"sync"
	"time"
)

const ttl = 10 * time.Minute

// intn 加密安全随机 [0,n)；表达式与抖动全部用它，避免弱随机告警。
func intn(n int) int {
	if n <= 0 {
		return 0
	}
	b, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(b.Int64())
}

type challenge struct {
	expr    string
	answer  string
	expires time.Time
}

var (
	once sync.Once
	mu   sync.Mutex
	m    = map[string]challenge{}
)

func startJanitor() {
	once.Do(func() {
		go func() {
			for range time.Tick(5 * time.Minute) {
				now := time.Now()
				mu.Lock()
				for k, c := range m {
					if now.After(c.expires) {
						delete(m, k)
					}
				}
				mu.Unlock()
			}
		}()
	})
}

func newID() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// New 生成挑战，返回 id 与答案（答案仅测试与日志调试用，勿输出给客户端）。
func New() (id, answer string) {
	startJanitor()
	a, b := 1+intn(9), 1+intn(9)
	var expr string
	if intn(2) == 0 {
		expr = fmt.Sprintf("%d + %d", a, b)
		answer = fmt.Sprint(a + b)
	} else {
		if b > a {
			a, b = b, a // 减法不出负数
		}
		expr = fmt.Sprintf("%d - %d", a, b)
		answer = fmt.Sprint(a - b)
	}
	id = newID()
	mu.Lock()
	if len(m) > 10000 {
		m = map[string]challenge{}
	}
	m[id] = challenge{expr: expr, answer: answer, expires: time.Now().Add(ttl)}
	mu.Unlock()
	return id, answer
}

// Verify 校验并消费：无论对错都作废该挑战，防同一题反复爆破。
func Verify(id, answer string) bool {
	if id == "" || answer == "" {
		return false
	}
	mu.Lock()
	c, ok := m[id]
	delete(m, id)
	mu.Unlock()
	return ok && time.Now().Before(c.expires) && answer == c.answer
}

var palette = []string{"#2e6ea6", "#8a5a00", "#a4482f", "#2f7d4f", "#5b4a9e"}

// Image 渲染挑战 SVG（数字带旋转/位移抖动 + 噪声曲线）；挑战不存在返回空串。
func Image(id string) string {
	mu.Lock()
	c, ok := m[id]
	mu.Unlock()
	if !ok {
		return ""
	}
	jr := intn // 抖动同样走加密随机（量大但每次渲染仅几十次，开销可忽略）
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="130" height="44" viewBox="0 0 130 44" role="img" aria-label="算术验证码">`)
	b.WriteString(`<rect width="130" height="44" fill="#f7fafd"/>`)
	for i := 0; i < 3; i++ {
		y := 6 + jr(32)
		fmt.Fprintf(&b, `<path d="M0 %d Q 40 %d 80 %d T 130 %d" fill="none" stroke="#d8e1ea" stroke-width="1.5"/>`, y, jr(44), y, jr(44))
	}
	x := 10.0
	for _, rch := range c.expr {
		if rch == ' ' {
			x += 7
			continue
		}
		rot := float64(jr(20)) - 10
		dy := float64(jr(6)) - 3
		fmt.Fprintf(&b,
			`<text x="%.1f" y="%.1f" transform="rotate(%.0f %.1f 24)" font-family="Georgia, 'Times New Roman', serif" font-size="27" font-weight="700" fill="%s">%s</text>`,
			x, 30+dy, rot, x, palette[jr(len(palette))], html.EscapeString(string(rch)))
		x += 21
	}
	b.WriteString(`</svg>`)
	return b.String()
}
