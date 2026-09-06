// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrTitleConflict = errors.New("title version or request conflict")
var ErrTitleInvalid = errors.New("invalid title configuration or operation")
var ErrTitleForbidden = errors.New("title or acceptance operation forbidden")

// First-time achievements use the same metric with target=1.
var TitleMetrics = []string{"threads_created", "replies_created", "likes_received", "featured_threads", "accepted_replies", "post_likes_max", "experience", "active_days", "registered_days", "email_verified"}

type TitleCondition struct {
	Metric  string `json:"metric"`
	Target  int64  `json:"target"`
	ForumID int64  `json:"forumId,string"`
}

type TitleDefinition struct {
	ID           int64            `json:"id,string"`
	Version      int64            `json:"version"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Badge        LevelBadge       `json:"badge"`
	Status       string           `json:"status"` // draft, active, paused (keep display), disabled
	Mode         string           `json:"mode"`   // automatic, manual
	Match        string           `json:"match"`  // all, any
	Conditions   []TitleCondition `json:"conditions"`
	StartsAt     *time.Time       `json:"startsAt"`
	EndsAt       *time.Time       `json:"endsAt"`
	DurationDays int              `json:"durationDays"`
	ExpiresAt    *time.Time       `json:"expiresAt"`
	Sort         int              `json:"sort"`
}

func (c TitleDefinition) Validate() error {
	if c.ID < 0 || c.Version < 0 || strings.TrimSpace(c.Name) == "" || utf8.RuneCountInString(c.Name) > 30 || utf8.RuneCountInString(c.Description) > 500 || c.Sort < 0 || c.Sort > 10000 {
		return ErrTitleInvalid
	}
	if c.Status != "draft" && c.Status != "active" && c.Status != "paused" && c.Status != "disabled" {
		return ErrTitleInvalid
	}
	if c.Mode != "automatic" && c.Mode != "manual" {
		return ErrTitleInvalid
	}
	if c.Match != "all" && c.Match != "any" {
		return ErrTitleInvalid
	}
	if len(c.Conditions) > 10 || (c.Mode == "automatic" && len(c.Conditions) == 0) || (c.Mode == "manual" && len(c.Conditions) != 0) {
		return ErrTitleInvalid
	}
	if c.DurationDays < 0 || c.DurationDays > 36500 || (c.DurationDays > 0 && c.ExpiresAt != nil) {
		return ErrTitleInvalid
	}
	if c.StartsAt != nil && c.EndsAt != nil && !c.EndsAt.After(*c.StartsAt) {
		return ErrTitleInvalid
	}
	if c.ExpiresAt != nil && c.StartsAt != nil && !c.ExpiresAt.After(*c.StartsAt) {
		return ErrTitleInvalid
	}
	if !colorRE.MatchString(c.Badge.Color) || !colorRE.MatchString(c.Badge.Background) || utf8.RuneCountInString(c.Badge.Label) > 20 {
		return ErrTitleInvalid
	}
	switch c.Badge.Icon {
	case "", "seedling", "star", "crown", "shield", "gem":
	default:
		return ErrTitleInvalid
	}
	seen := map[TitleCondition]bool{}
	for _, v := range c.Conditions {
		known := false
		for _, metric := range TitleMetrics {
			if metric == v.Metric {
				known = true
			}
		}
		if !known || v.Target < 1 || v.Target > 1e12 || v.ForumID < 0 || seen[v] {
			return ErrTitleInvalid
		}
		seen[v] = true
		switch v.Metric {
		case "experience", "active_days", "registered_days", "email_verified":
			if v.ForumID != 0 {
				return ErrTitleInvalid
			}
		}
		if v.Metric == "email_verified" && v.Target != 1 {
			return ErrTitleInvalid
		}
	}
	return nil
}

func (c TitleDefinition) issuing(now time.Time) bool {
	return c.Status == "active" && (c.StartsAt == nil || !now.Before(*c.StartsAt)) && (c.EndsAt == nil || now.Before(*c.EndsAt)) && (c.ExpiresAt == nil || now.Before(*c.ExpiresAt))
}

func (c TitleDefinition) matches(counts []int64) bool {
	if len(counts) != len(c.Conditions) || len(counts) == 0 {
		return false
	}
	met := 0
	for i, v := range c.Conditions {
		if counts[i] >= v.Target {
			met++
		}
	}
	return (c.Match == "all" && met == len(counts)) || (c.Match == "any" && met > 0)
}

func (c TitleDefinition) expiry(now time.Time) *time.Time {
	if c.DurationDays > 0 {
		t := now.Add(time.Duration(c.DurationDays) * 24 * time.Hour)
		return &t
	}
	return c.ExpiresAt
}
