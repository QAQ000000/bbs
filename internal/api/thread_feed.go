package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"dzforum/internal/store"
)

func (s *Server) threadFeed(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fid, size := queryID(r, "forumId"), s.sets(r).ThreadsPerPage
	if fid < 0 || q.Get("sort") != "" || q.Get("page") != "" {
		s.fail(w, r, 422, "VALIDATION_FAILED", "Cursor pagination requires default sorting and no page number")
		return
	}
	var after *store.ThreadCursor
	if raw := q.Get("cursor"); raw != "" {
		b, err := base64.RawURLEncoding.DecodeString(raw)
		var v store.ThreadCursor
		if len(raw) > 512 || err != nil || json.Unmarshal(b, &v) != nil || v.ID <= 0 || v.LastPostAt.IsZero() || v.ForumID != fid {
			s.fail(w, r, 422, "VALIDATION_FAILED", "Invalid cursor")
			return
		}
		after = &v
	}
	if fid > 0 {
		if _, err := s.st.Forum(r.Context(), fid); s.readError(w, r, err) {
			return
		}
	}
	rows, more, err := s.st.ThreadFeed(r.Context(), fid, size, after)
	if s.readError(w, r, err) {
		return
	}
	threads, err := s.memberThreadRows(r, rows)
	if s.readError(w, r, err) {
		return
	}
	data := map[string]any{"threads": threads, "stickies": []map[string]any{}}
	if fid > 0 {
		sticky, err := s.st.Stickies(r.Context(), fid)
		if s.readError(w, r, err) {
			return
		}
		data["stickies"], err = s.memberThreadRows(r, sticky)
		if s.readError(w, r, err) {
			return
		}
	}
	next := ""
	if more {
		last := rows[len(rows)-1]
		b, _ := json.Marshal(store.ThreadCursor{ForumID: fid, ID: last.ID, LastPostAt: last.LastPostAt})
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	writeJSON(w, 200, map[string]any{"data": data, "meta": map[string]any{"pagination": "cursor", "pageSize": size, "hasMore": more, "nextCursor": next}})
}
