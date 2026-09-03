// SPDX-License-Identifier: AGPL-3.0-or-later
// http.go：HTTP 服务器参数（超时与 SSE 长连接的写策略）。
package main

import (
	"net/http"
	"time"
)

func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // SSE 长连接不受写超时限制，由每笔写操作的 deadline 控制
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
}
