package api

import "testing"

// 单个 / 全部会话擦除的路由区分：只有“没有 sessionId 通配符”才是全部撤销，
// 非法输入必须判为无效，不能退化成撤销全部。
func TestParseAdminSessionTarget(t *testing.T) {
	cases := []struct {
		raw    string
		target int64
		all    bool
		valid  bool
	}{
		{"", 0, true, true},
		{"12", 12, false, true},
		{"+7", 7, false, true},
		{"abc", 0, false, false},
		{"0", 0, false, false},
		{"-1", 0, false, false},
		{"1e3", 0, false, false},
		{" 12", 0, false, false},
		{"99999999999999999999", 0, false, false},
	}
	for _, c := range cases {
		target, all, valid := parseAdminSessionTarget(c.raw)
		if target != c.target || all != c.all || valid != c.valid {
			t.Fatalf("parseAdminSessionTarget(%q) = (%d, %v, %v), want (%d, %v, %v)",
				c.raw, target, all, valid, c.target, c.all, c.valid)
		}
	}
}
