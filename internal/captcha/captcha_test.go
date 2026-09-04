// SPDX-License-Identifier: AGPL-3.0-or-later
package captcha

import (
	"strings"
	"testing"
)

func TestCaptchaVerify(t *testing.T) {
	id, answer := New()
	if answer == "" || len(id) < 10 {
		t.Fatalf("挑战生成异常: id=%q", id)
	}
	if !Verify(id, answer) {
		t.Fatal("正确答案应通过")
	}
	if Verify(id, answer) {
		t.Fatal("一次性：同一挑战第二次校验应失败")
	}
	// 错误答案也消费挑战
	id2, _ := New()
	if Verify(id2, "0") {
		t.Fatal("错误答案不应通过")
	}
	if Verify(id2, "1") {
		t.Fatal("错误尝试后挑战应已作废")
	}
	// 空输入
	if Verify("", "") || Verify(id2, "") {
		t.Fatal("空 id/答案不应通过")
	}
}

func TestCaptchaImage(t *testing.T) {
	id, _ := New()
	svg := Image(id)
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "</svg>") {
		t.Fatalf("SVG 渲染异常: %.80s", svg)
	}
	if Image("nonexistent-id") != "" {
		t.Fatal("不存在的挑战应返回空串")
	}
}
