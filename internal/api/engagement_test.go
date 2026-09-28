package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"dzforum/internal/store"
)

func engagementAPIFixture(t *testing.T) store.EngagementConfig {
	t.Helper()
	requireDB(t)
	c, err := smokeSrv.st.EngagementConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { flowSQL(t, `UPDATE engagement_config SET version=$1,body=$2 WHERE id`, c.Version, raw) })
	return c
}
func TestPollAPIAndIdempotency(t *testing.T) {
	c := engagementAPIFixture(t)
	mc := memberAPIConfig(t)
	mc.Levels[0].Permissions["post.skip.moderate"] = true
	setMemberAPIConfig(t, mc)
	author, cookie, csrf := memberTestUser(t)
	th, _, err := smokeSrv.st.CreateThread(context.Background(), 1, author.ID, author.Username, "poll test", "poll body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/threads/%d/poll", th.ID)
	in := store.PollInput{Question: "选择方案", Options: []string{"甲", "乙", "丙"}, MaxChoices: 2, DurationHours: 24}
	checkJSON(t, memberJSON(t, "POST", path, in, "", cookie), 403)
	checkJSON(t, memberJSON(t, "POST", path, in, userCSRF, userCookie), 403)
	checkJSON(t, memberJSON(t, "POST", path, in, csrf, cookie), 201)
	checkJSON(t, memberJSON(t, "POST", path, in, csrf, cookie), 409)
	checkJSON(t, smokeGet(t, path, nil), 200)
	checkJSON(t, memberJSON(t, "PUT", path+"/vote", map[string]any{"optionIds": []int{1, 1}}, userCSRF, userCookie), 422)
	checkJSON(t, memberJSON(t, "PUT", path+"/vote", map[string]any{"optionIds": []int{4}}, userCSRF, userCookie), 422)
	for _, ids := range [][]int{{1, 2}, {2, 1}} {
		checkJSON(t, memberJSON(t, "PUT", path+"/vote", map[string]any{"optionIds": ids}, userCSRF, userCookie), 200)
	}
	p, err := smokeSrv.st.Poll(context.Background(), th.ID, 2)
	if err != nil || p.Voters != 1 || p.Options[0].Votes != 1 || p.Options[1].Votes != 1 {
		t.Fatal(p, err)
	}
	checkJSON(t, memberJSON(t, "PUT", path+"/vote", map[string]any{"optionIds": []int{3}}, userCSRF, userCookie), 409)
	mc.Levels[0].Permissions["poll.vote"] = false
	setMemberAPIConfig(t, mc)
	checkJSON(t, memberJSON(t, "PUT", path+"/vote", map[string]any{"optionIds": []int{3}}, csrf, cookie), 403)
	mc.Levels[0].Permissions["poll.vote"] = true
	setMemberAPIConfig(t, mc)
	checkJSON(t, memberJSON(t, "POST", path+"/close", map[string]any{}, userCSRF, userCookie), 403)
	checkJSON(t, memberJSON(t, "POST", path+"/close", map[string]any{}, csrf, cookie), 200)
	checkJSON(t, memberJSON(t, "PUT", path+"/vote", map[string]any{"optionIds": []int{3}}, csrf, cookie), 409)
	checkJSON(t, smokeGet(t, "/api/v1/admin/engagement/config", userCookie), 403)
	c.Poll.MaxOptions = 5
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/admin/engagement/config", c, adminCSRF, adminCookie), 200)
	checkJSON(t, memberJSON(t, "PUT", "/api/v1/admin/engagement/config", c, adminCSRF, adminCookie), 409)
}
func TestPollModerationAndConcurrentVotes(t *testing.T) {
	engagementAPIFixture(t)
	author, cookie, _ := memberTestUser(t)
	ctx := context.Background()
	th, _, err := smokeSrv.st.CreateThread(ctx, 1, author.ID, author.Username, "pending poll", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	in := store.PollInput{Question: "审核投票", Options: []string{"yes", "no"}, MaxChoices: 1, DurationHours: 1}
	if err = smokeSrv.st.CreatePoll(ctx, th.ID, author.ID, in, true, "review"); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/threads/%d/poll", th.ID)
	checkJSON(t, smokeGet(t, path, nil), 404)
	checkJSON(t, smokeGet(t, path, cookie), 200)
	checkJSON(t, memberJSON(t, "PUT", path+"/vote", map[string]any{"optionIds": []int{1}}, userCSRF, userCookie), 409)
	checkJSON(t, smokeGet(t, "/api/v1/admin/polls", adminCookie), 200)
	checkJSON(t, memberJSON(t, "POST", fmt.Sprintf("/api/v1/admin/polls/%d/moderation", th.ID), map[string]any{"action": "approve", "reason": "通过"}, adminCSRF, adminCookie), 200)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- smokeSrv.st.VotePoll(ctx, th.ID, author.ID, []int32{1}) }()
	}
	wg.Wait()
	close(errs)
	for err = range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err := smokeSrv.st.Poll(ctx, th.ID, author.ID)
	if err != nil || p.Voters != 1 || p.Options[0].Votes != 1 {
		t.Fatal(p, err)
	}
	// Voting alone must not make an otherwise deletable account undeletable.
	voter, err := smokeSrv.st.CreateUser(ctx, "poll_only_"+t.Name(), "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = smokeSrv.st.VotePoll(ctx, th.ID, voter.ID, []int32{1}); err != nil {
		t.Fatal(err)
	}
	if err = smokeSrv.st.DeleteUser(ctx, voter.ID); err != nil {
		t.Fatal("voter deletion", err)
	}
	p, err = smokeSrv.st.Poll(ctx, th.ID, author.ID)
	if err != nil || p.Voters != 2 || p.Options[0].Votes != 2 {
		t.Fatal("anonymous totals changed", p, err)
	}
	flowSQL(t, `UPDATE thread_polls SET closes_at=now()-interval '1 second' WHERE thread_id=$1`, th.ID)
	checkJSON(t, memberJSON(t, "PUT", path+"/vote", map[string]any{"optionIds": []int{1}}, userCSRF, userCookie), 409)
	mc := memberAPIConfig(t)
	mc.Forums = append(mc.Forums, store.ForumMembership{ForumID: 1, MinimumLevel: 4, MembersOnly: true})
	setMemberAPIConfig(t, mc)
	checkJSON(t, smokeGet(t, path, cookie), 404)
}
