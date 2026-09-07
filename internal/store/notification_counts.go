package store

import "context"

// NotificationCounts applies fresh per-user visibility to a bounded group, after delivery commits.
func (s *Store) NotificationCounts(ctx context.Context, ids []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	a, err := readAudience(ctx, s.pool, ids, "", 0, 0)
	if err != nil {
		return nil, err
	}
	for _, uid := range ids {
		u, ok := a.Users[uid]
		if !ok || u.Blocked || u.LevelID == nil {
			continue
		}
		if _, ok := a.Config.Level(*u.LevelID); ok {
			out[uid] = 0
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT n.uid,n.scope,coalesce(t.forum_id,0),count(*)
	 FROM notifications n LEFT JOIN threads t ON t.id=n.thread_id
	 LEFT JOIN posts p ON p.id=n.post_id AND p.thread_id=t.id
	 WHERE n.uid=ANY($1) AND NOT n.read AND (n.scope='account' OR
	 (NOT t.deleted AND NOT t.pending AND NOT p.deleted AND NOT p.pending))
	 GROUP BY n.uid,n.scope,t.forum_id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid, fid, n int64
		var scope string
		if err = rows.Scan(&uid, &scope, &fid, &n); err != nil {
			return nil, err
		}
		if _, ok := out[uid]; ok && (scope == "account" || a.CanReadForum(uid, fid)) {
			out[uid] += n
		}
	}
	return out, rows.Err()
}
