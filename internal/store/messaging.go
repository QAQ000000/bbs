// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrMessageBlocked = errors.New("message blocked")
var ErrMessageReplyRequired = errors.New("recipient reply required")
var ErrCommunityInvalid = errors.New("invalid community operation")
var ErrCommunityConflict = errors.New("community version conflict")
var ErrMessageRateLimited = errors.New("message rate limit exceeded")

type Conversation struct {
	ID              int64     `json:"id,string"`
	OtherID         int64     `json:"otherId,string"`
	OtherName       string    `json:"otherName"`
	LastMessageAt   time.Time `json:"lastMessageAt"`
	Unread          int64     `json:"unread"`
	Blocked         bool      `json:"blocked"`
	WaitingForReply bool      `json:"waitingForReply"`
}
type Message struct {
	ID             int64     `json:"id,string"`
	ConversationID int64     `json:"conversationId,string"`
	SenderID       int64     `json:"senderId,string"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Pair locks cover the absent-row case as well as concurrent block/follow/send.
func lockUserPair(ctx context.Context, tx pgx.Tx, a, b int64) error {
	if a > b {
		a, b = b, a
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("community-pair:%d:%d", a, b))
	return err
}

func (s *Store) SendDirectMessage(ctx context.Context, sender, recipient int64, body string) (Message, error) {
	var msg Message
	body = strings.TrimSpace(body)
	if sender <= 0 || recipient <= 0 || sender == recipient || !utf8.ValidString(body) || utf8.RuneCountInString(body) == 0 || utf8.RuneCountInString(body) > 5000 {
		return msg, ErrCommunityInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return msg, err
	}
	defer tx.Rollback(ctx)
	// Per-sender locks make quotas hold across processes and different recipients.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("message-sender:%d", sender)); err != nil {
		return msg, err
	}
	if err = lockUserPair(ctx, tx, sender, recipient); err != nil {
		return msg, err
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT count(*)=2 FROM users WHERE id IN ($1,$2) AND (blocked_until IS NULL OR blocked_until<=now())`, sender, recipient).Scan(&valid); err != nil {
		return msg, err
	}
	if !valid {
		return msg, ErrNotFound
	}
	var cid, initiator, first int64
	var replied bool
	err = tx.QueryRow(ctx, `SELECT conversation_id,initiator_id,first_message_id,recipient_replied_at IS NOT NULL
 FROM conversation_pair_states WHERE least(initiator_id,recipient_id)=least($1::bigint,$2::bigint)
 AND greatest(initiator_id,recipient_id)=greatest($1::bigint,$2::bigint) FOR UPDATE`, sender, recipient).Scan(&cid, &initiator, &first, &replied)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return msg, err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM conversations c JOIN conversation_pair_states p ON p.conversation_id=c.id
  WHERE p.initiator_id=$1 AND c.created_at>=now()-interval '24 hours'`, sender).Scan(&count); err != nil {
			return msg, err
		}
		if count >= 20 {
			return msg, ErrMessageRateLimited
		}
		if err = tx.QueryRow(ctx, `INSERT INTO conversations DEFAULT VALUES RETURNING id`).Scan(&cid); err != nil {
			return msg, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO conversation_members(conversation_id,uid) VALUES($1,$2),($1,$3)`, cid, sender, recipient); err != nil {
			return msg, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO conversation_pair_states(conversation_id,initiator_id,recipient_id) VALUES($1,$2,$3)`, cid, sender, recipient); err != nil {
			return msg, err
		}
		initiator = sender
	}
	var blocked bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND blocked)`, cid).Scan(&blocked); err != nil {
		return msg, err
	}
	if blocked {
		return msg, ErrMessageBlocked
	}
	if initiator == sender && first != 0 && !replied {
		return msg, ErrMessageReplyRequired
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM messages WHERE sender_id=$1 AND created_at>now()-interval '1 minute'`, sender).Scan(&recent); err != nil {
		return msg, err
	}
	if recent >= 30 {
		return msg, ErrMessageRateLimited
	}
	err = tx.QueryRow(ctx, `INSERT INTO messages(conversation_id,sender_id,body) VALUES($1,$2,$3)
 RETURNING id,conversation_id,sender_id,body,created_at`, cid, sender, body).Scan(&msg.ID, &msg.ConversationID, &msg.SenderID, &msg.Body, &msg.CreatedAt)
	if err != nil {
		return msg, err
	}
	if _, err = tx.Exec(ctx, `UPDATE conversation_pair_states SET first_message_id=CASE WHEN first_message_id=0 THEN $2 ELSE first_message_id END,
 recipient_replied_at=CASE WHEN initiator_id<>$3 THEN coalesce(recipient_replied_at,now()) ELSE recipient_replied_at END,
 unrestricted_at=CASE WHEN initiator_id<>$3 THEN coalesce(unrestricted_at,now()) ELSE unrestricted_at END WHERE conversation_id=$1`, cid, msg.ID, sender); err != nil {
		return msg, err
	}
	if _, err = tx.Exec(ctx, `UPDATE conversations SET last_message_at=now() WHERE id=$1`, cid); err != nil {
		return msg, err
	}
	return msg, tx.Commit(ctx)
}

func (s *Store) SetConversationBlocked(ctx context.Context, cid, uid int64, blocked bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var other int64
	err = tx.QueryRow(ctx, `SELECT CASE WHEN initiator_id=$2 THEN recipient_id ELSE initiator_id END FROM conversation_pair_states
 WHERE conversation_id=$1 AND $2 IN (initiator_id,recipient_id)`, cid, uid).Scan(&other)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err = lockUserPair(ctx, tx, uid, other); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE conversation_members SET blocked=$3,blocked_at=CASE WHEN $3 THEN now() ELSE NULL END WHERE conversation_id=$1 AND uid=$2`, cid, uid, blocked); err != nil {
		return err
	}
	if blocked {
		if _, err = tx.Exec(ctx, `DELETE FROM user_follows WHERE (follower_id=$1 AND following_id=$2) OR (follower_id=$2 AND following_id=$1)`, uid, other); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) Conversations(ctx context.Context, uid int64, page int) ([]Conversation, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM conversation_members WHERE uid=$1`, uid).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id,other.uid,u.username,c.last_message_at,
 (SELECT count(*) FROM messages m WHERE m.conversation_id=c.id AND m.sender_id<>$1 AND m.id>me.last_read_message_id AND m.deleted_at IS NULL),
 me.blocked, ps.initiator_id=$1 AND ps.first_message_id>0 AND ps.recipient_replied_at IS NULL
 FROM conversations c JOIN conversation_members me ON me.conversation_id=c.id AND me.uid=$1
 JOIN conversation_members other ON other.conversation_id=c.id AND other.uid<>$1 JOIN users u ON u.id=other.uid
 JOIN conversation_pair_states ps ON ps.conversation_id=c.id ORDER BY c.last_message_at DESC,c.id DESC LIMIT 30 OFFSET $2`, uid, (page-1)*30)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var v Conversation
		if err := rows.Scan(&v.ID, &v.OtherID, &v.OtherName, &v.LastMessageAt, &v.Unread, &v.Blocked, &v.WaitingForReply); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

func (s *Store) ConversationMessages(ctx context.Context, uid, cid, before int64) ([]Message, error) {
	var member bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members WHERE uid=$1 AND conversation_id=$2)`, uid, cid).Scan(&member); err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT id,conversation_id,sender_id,body,created_at FROM messages
 WHERE conversation_id=$1 AND deleted_at IS NULL AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT 50`, cid, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ReadConversation(ctx context.Context, uid, cid, mid int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE conversation_members SET last_read_message_id=greatest(last_read_message_id,$3)
 WHERE uid=$1 AND conversation_id=$2 AND EXISTS(SELECT 1 FROM messages WHERE id=$3 AND conversation_id=$2)`, uid, cid, mid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
