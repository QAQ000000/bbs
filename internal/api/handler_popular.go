package api

import (
	"net/http"
	"time"
)

func (s *Server) homePopular(w http.ResponseWriter, r *http.Request) {
	v, err := s.st.Popular(r.Context(), time.Now())
	if err != nil {
		s.fail(w, r, 503, "POPULAR_UNAVAILABLE", "热门内容暂不可用")
		return
	}
	s.respond(w, 200, v)
}
