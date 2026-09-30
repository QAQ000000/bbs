package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"dzforum/internal/store"
)

// feedCursorPayload 只包含翻页所需的键与归属信息，签名后不可伪造。
type feedCursorPayload struct {
	UserID    int64  `json:"u"`
	Stream    string `json:"s"`
	Sort      string `json:"o"`
	CreatedAt int64  `json:"t"`
	ID        int64  `json:"i"`
}

const feedCursorSort = "created"

func (s *Server) encodeFeedCursor(uid int64, stream string, createdAt time.Time, id int64) string {
	body, _ := json.Marshal(feedCursorPayload{UserID: uid, Stream: stream, Sort: feedCursorSort, CreatedAt: createdAt.UTC().UnixNano(), ID: id})
	mac := hmac.New(sha256.New, s.feedKey)
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// decodeFeedCursor 校验 HMAC，并绑定当前账号、流类型与排序。
// 跨流、跨账号或篡改过的游标一律判为无效。
func (s *Server) decodeFeedCursor(raw string, uid int64, stream string) (*store.FeedCursor, bool) {
	if raw == "" || len(raw) > 512 {
		return nil, false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return nil, false
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, s.feedKey)
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, false
	}
	var p feedCursorPayload
	if json.Unmarshal(body, &p) != nil || p.UserID != uid || p.Stream != stream || p.Sort != feedCursorSort || p.ID <= 0 || p.CreatedAt <= 0 {
		return nil, false
	}
	return &store.FeedCursor{UserID: p.UserID, Stream: p.Stream, Sort: p.Sort, CreatedAt: time.Unix(0, p.CreatedAt).UTC(), ID: p.ID}, true
}
