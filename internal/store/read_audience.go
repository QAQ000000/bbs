package store

import (
	"context"
	"encoding/json"
	"slices"

	"dzforum/internal/perm"
	"github.com/jackc/pgx/v5"
)

type ReadUser struct {
	ID          int64
	GroupID     int
	LevelID     *int
	Blocked     bool
	Moderates   []int64
	Preferences map[string]bool
}

type ReadAudience struct {
	Config       MembershipConfig
	Settings     SiteSettings
	Forums       []int64
	Users        map[int64]ReadUser
	SessionValid bool
	Thread       *Thread
}

type audienceReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// One SQL snapshot serves both live authorization and bounded subscription batches.
// It contains no passwords, session tokens, message bodies or posting quotas.
func readAudience(ctx context.Context, q audienceReader, ids []int64, token string, sessionUID, threadID int64) (ReadAudience, error) {
	var a ReadAudience
	var configRaw, settingsRaw, usersRaw, threadRaw []byte
	err := q.QueryRow(ctx, `SELECT c.version,c.body,
	 (SELECT coalesce(jsonb_object_agg(key,value),'{}') FROM settings),
	 ARRAY(SELECT id::bigint FROM forums ORDER BY id),
	 (SELECT coalesce(jsonb_agg(jsonb_build_object(
	 'ID',u.id,'GroupID',u.group_id,'LevelID',ms.level_id,
	 'Blocked',coalesce(u.blocked_until>now(),false),
	 'Moderates',ARRAY(SELECT forum_id::bigint FROM forum_moderators WHERE user_id=u.id),
	 'Preferences',coalesce(np.body,'{}'))),'[]')
	 FROM users u LEFT JOIN member_states ms ON ms.user_id=u.id
	 LEFT JOIN notification_preferences np ON np.uid=u.id WHERE u.id=ANY($1)),
	 ($3::bigint=0 OR EXISTS(SELECT 1 FROM sessions WHERE token=$2 AND user_id=$3
	 AND expires_at>now() AND revoked_at IS NULL)),
	 (SELECT jsonb_build_object('ID',id,'ForumID',forum_id,'AuthorID',author_id,'Pending',pending)
	 FROM threads WHERE id=$4 AND NOT deleted)
	 FROM membership_config c WHERE c.id`, ids, hashToken(token), sessionUID, threadID).
		Scan(&a.Config.Version, &configRaw, &settingsRaw, &a.Forums, &usersRaw, &a.SessionValid, &threadRaw)
	if err != nil {
		return a, err
	}
	version := a.Config.Version
	if err = json.Unmarshal(configRaw, &a.Config); err != nil {
		return a, err
	}
	a.Config.Version = version
	a.Config.Normalize()
	var values map[string]string
	if err = json.Unmarshal(settingsRaw, &values); err != nil {
		return a, err
	}
	if a.Settings, err = settingsFromValues(values); err != nil {
		return a, err
	}
	var users []ReadUser
	if err = json.Unmarshal(usersRaw, &users); err != nil {
		return a, err
	}
	a.Users = make(map[int64]ReadUser, len(users))
	for _, u := range users {
		a.Users[u.ID] = u
	}
	if len(threadRaw) > 0 {
		err = json.Unmarshal(threadRaw, &a.Thread)
	}
	return a, err
}

func (s *Store) LiveReadAudience(ctx context.Context, token string, uid, threadID int64) (ReadAudience, error) {
	ids := []int64{}
	if uid > 0 {
		ids = append(ids, uid)
	}
	return readAudience(ctx, s.pool, ids, token, uid, threadID)
}

// ForumReadAllowed is shared with the ordinary API membership resolver.
func ForumReadAllowed(c MembershipConfig, level *MemberLevel, admin, moderator bool, fid int64) bool {
	allow, rank := c.GuestPermissions["forum.read"], 0
	if level != nil {
		allow, rank = level.Permissions["forum.read"], level.Rank
	}
	if f, ok := c.Forum(fid); ok {
		minimum, exists := c.Level(f.MinimumLevel)
		allow = allow && exists && rank >= minimum.Rank && (!f.MembersOnly || level != nil) && !slices.Contains(f.Denied, "forum.read")
	}
	return allow || admin || moderator
}

func (a ReadAudience) CanReadForum(uid, fid int64) bool {
	if !slices.Contains(a.Forums, fid) {
		return false
	}
	if uid == 0 {
		return ForumReadAllowed(a.Config, nil, false, false, fid)
	}
	u, ok := a.Users[uid]
	if !ok || u.Blocked || u.LevelID == nil {
		return false
	}
	level, ok := a.Config.Level(*u.LevelID)
	if !ok {
		return false
	}
	role := perm.RoleFromGroupID(u.GroupID)
	return ForumReadAllowed(a.Config, &level, perm.Allowed(role, perm.AdminPanel), perm.Allowed(role, perm.ContentModerate) && slices.Contains(u.Moderates, fid), fid)
}

func (a ReadAudience) CanReadThread(uid int64) bool {
	t := a.Thread
	if t == nil || !a.CanReadForum(uid, t.ForumID) {
		return false
	}
	if !t.Pending {
		return true
	}
	if uid == 0 {
		return false
	}
	u := a.Users[uid]
	role := perm.RoleFromGroupID(u.GroupID)
	return uid == t.AuthorID || perm.Allowed(role, perm.ContentModerate) && (perm.Allowed(role, perm.AdminPanel) || slices.Contains(u.Moderates, t.ForumID))
}

func (a ReadAudience) Preference(uid int64, key string) bool {
	u, ok := a.Users[uid]
	if !ok {
		return false
	}
	v, set := u.Preferences[key]
	return !set || v
}
