// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import "fmt"

// ThreadURL 生成帖子页伪静态地址（第三段与旧版一致，固定为 1）。
func ThreadURL(tid int64, page int) string {
	if page <= 1 {
		page = 1
	}
	return fmt.Sprintf("/thread-%d-%d-1.html", tid, page)
}

// clampPage 修正非法页码。
func clampPage(p int) int {
	if p < 1 {
		return 1
	}
	if p > 10000 {
		return 10000
	}
	return p
}
