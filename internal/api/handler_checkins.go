package api

import "net/http"

func (s *Server) checkinGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	v, err := s.st.CheckinStatus(r.Context(), User(r).ID)
	if !s.engagementError(w, r, err) {
		s.respond(w, 200, v)
	}
}
func (s *Server) checkinHistory(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	rows, err := s.st.CheckinHistory(r.Context(), User(r).ID, r.URL.Query().Get("before"))
	if s.engagementError(w, r, err) {
		return
	}
	next := ""
	if len(rows) > 30 {
		rows = rows[:30]
		next = rows[29].Day
	}
	s.respond(w, 200, map[string]any{"items": rows, "nextBefore": next})
}
func (s *Server) checkinClaim(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !s.membershipJSON(w, r, &req) || !s.engagementMember(w, r, "checkin.claim", nil) {
		return
	}
	v, err := s.st.ClaimCheckin(r.Context(), User(r).ID)
	if !s.engagementError(w, r, err) {
		s.respond(w, 200, v)
	}
}
func (s *Server) checkinRoutes() {
	s.mux.HandleFunc("GET /api/v1/me/checkin", s.checkinGet)
	s.mux.HandleFunc("GET /api/v1/me/checkins", s.checkinHistory)
	s.mux.HandleFunc("POST /api/v1/me/checkin", s.checkinClaim)
}
