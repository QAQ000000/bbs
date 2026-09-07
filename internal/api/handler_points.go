package api

import (
	"errors"
	"net/http"
	"strconv"

	"dzforum/internal/store"
)

func (s *Server) pointsError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, store.ErrPointsInvalid):
		s.fail(w, r, 422, "POINTS_INVALID", "积分参数无效")
	case errors.Is(err, store.ErrPointsConflict):
		s.fail(w, r, 409, "POINTS_CONFLICT", "积分版本已变化或请求标识冲突")
	case errors.Is(err, store.ErrPointsInsufficient):
		s.fail(w, r, 409, "POINTS_INSUFFICIENT", "可用积分不足")
	case errors.Is(err, store.ErrNotFound):
		s.fail(w, r, 404, "NOT_FOUND", "用户不存在")
	default:
		s.log.Error("points operation failed", "err", err)
		s.fail(w, r, 503, "POINTS_UNAVAILABLE", "积分服务暂不可用")
	}
	return true
}

func (s *Server) pointsAccountGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	uid := User(r).ID
	if r.PathValue("uid") != "" {
		uid = pathID(r, "uid")
	}
	v, err := s.st.PointsAccount(r.Context(), uid)
	if !s.pointsError(w, r, err) {
		s.respond(w, 200, v)
	}
}

func (s *Server) pointsLedgerGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLogin(w, r) {
		return
	}
	uid := User(r).ID
	if r.PathValue("uid") != "" {
		uid = pathID(r, "uid")
	}
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			s.pointsError(w, r, store.ErrPointsInvalid)
			return
		}
	}
	if _, err := s.st.PointsAccount(r.Context(), uid); s.pointsError(w, r, err) {
		return
	}
	rows, err := s.st.PointsLedger(r.Context(), uid, before)
	if s.pointsError(w, r, err) {
		return
	}
	next := ""
	if len(rows) > 50 {
		rows = rows[:50]
		next = idString(rows[49].ID)
	}
	s.respond(w, 200, map[string]any{"items": rows, "nextBefore": next})
}

func (s *Server) pointsConfigGet(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.PointsConfig(r.Context())
	if !s.pointsError(w, r, err) {
		s.respond(w, 200, c)
	}
}
func (s *Server) pointsConfigSave(w http.ResponseWriter, r *http.Request) {
	var c store.PointsConfig
	if !s.membershipJSON(w, r, &c) {
		return
	}
	if s.pointsError(w, r, s.st.SavePointsConfig(r.Context(), c, User(r).ID)) {
		return
	}
	c.Version++
	s.respond(w, 200, c)
}
func (s *Server) pointsAdjust(w http.ResponseWriter, r *http.Request) {
	var v store.PointsAdjustment
	if !s.membershipJSON(w, r, &v) {
		return
	}
	if s.pointsError(w, r, s.st.AdjustPoints(r.Context(), pathID(r, "uid"), User(r).ID, v)) {
		return
	}
	s.pointsAccountGet(w, r)
}
func (s *Server) pointsReconcile(w http.ResponseWriter, r *http.Request) {
	v, err := s.st.ReconcilePoints(r.Context(), pathID(r, "uid"))
	if !s.pointsError(w, r, err) {
		s.respond(w, 200, v)
	}
}
func (s *Server) pointsRoutes() {
	s.mux.HandleFunc("GET /api/v1/me/points", s.pointsAccountGet)
	s.mux.HandleFunc("GET /api/v1/me/points/ledger", s.pointsLedgerGet)
	for route, h := range map[string]http.HandlerFunc{
		"GET /api/v1/admin/points/config":                s.pointsConfigGet,
		"PUT /api/v1/admin/points/config":                s.pointsConfigSave,
		"GET /api/v1/admin/points/users/{uid}":           s.pointsAccountGet,
		"GET /api/v1/admin/points/users/{uid}/ledger":    s.pointsLedgerGet,
		"POST /api/v1/admin/points/users/{uid}/adjust":   s.pointsAdjust,
		"GET /api/v1/admin/points/users/{uid}/reconcile": s.pointsReconcile,
	} {
		s.mux.HandleFunc(route, s.adminPointGuard(route, h))
	}
}
