package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

func TestEngagementThreadPresentation(t *testing.T) {
	c := engagementAPIFixture(t)
	mc := memberAPIConfig(t)
	ctx := context.Background()
	owner, cookie, _ := memberTestUser(t)
	doc := loadAPIContract(t)
	rules := assertContractResponse(t, doc, "GET", "/api/v1/engagement/rules", smokeGet(t, "/api/v1/engagement/rules", nil), 200)["data"].(contractObject)
	if len(rules) != 3 || rules["version"] != nil {
		t.Fatal("public rules contain admin fields", rules)
	}
	th, _, err := smokeSrv.st.CreateThread(ctx, 1, owner.ID, owner.Username, "engagement presentation", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/threads/%d", th.ID)
	get := func(c *http.Cookie) contractObject {
		return assertContractResponse(t, doc, "GET", "/api/v1/threads/{tid}", smokeGet(t, path, c), 200)["data"].(contractObject)
	}
	caps := get(cookie)["capabilities"].(contractObject)
	if caps["canCreatePoll"] != true || caps["canCreateBounty"] != true {
		t.Fatal(caps)
	}
	if err = smokeSrv.st.CreatePoll(ctx, th.ID, owner.ID, store.PollInput{Question: "private pending question", Options: []string{"yes", "no"}, MaxChoices: 1, DurationHours: 24}, true, "moderation"); err != nil {
		t.Fatal(err)
	}
	if err = smokeSrv.st.AdjustPoints(ctx, owner.ID, 1, store.PointsAdjustment{Version: 1, Delta: 20, Reason: "seed", Key: "presentation-seed"}); err != nil {
		t.Fatal(err)
	}
	if err = smokeSrv.st.CreateBounty(ctx, th.ID, owner.ID, store.BountyInput{Amount: 10, DurationHours: 24}); err != nil {
		t.Fatal(err)
	}
	guest := get(nil)
	if guest["poll"] != nil || guest["bounty"] == nil || guest["capabilities"].(contractObject)["canVote"] != false {
		t.Fatal("pending poll leaked", guest)
	}
	own := get(cookie)
	if own["poll"] == nil || own["capabilities"].(contractObject)["canClosePoll"] != true {
		t.Fatal(own)
	}
	for _, suffix := range []string{"?forumId=1", "?forumId=1&pagination=cursor"} {
		body := assertContractResponse(t, doc, "GET", "/api/v1/threads", smokeGet(t, "/api/v1/threads"+suffix, nil), 200)["data"].(contractObject)
		found := false
		for _, raw := range body["threads"].([]any) {
			row := raw.(contractObject)
			if row["id"] == idString(th.ID) {
				found = true
				if row["poll"] != nil || row["bounty"] == nil {
					t.Fatal("list leaked pending poll", row)
				}
			}
		}
		if !found {
			t.Fatal("missing list item")
		}
	}
	if err = smokeSrv.st.ManagePoll(ctx, th.ID, 1, true, "approve", "checked"); err != nil {
		t.Fatal(err)
	}
	if get(userCookie)["capabilities"].(contractObject)["canVote"] != true {
		t.Fatal("vote unavailable")
	}
	if err = smokeSrv.st.VotePoll(ctx, th.ID, owner.ID, []int32{1}); err != nil {
		t.Fatal(err)
	}
	if get(cookie)["capabilities"].(contractObject)["canVote"] != false {
		t.Fatal("duplicate vote offered")
	}
	c.Poll.Enabled = false
	c.Bounty.Enabled = false
	if err = smokeSrv.st.SaveEngagementConfig(ctx, c, 1); err != nil {
		t.Fatal(err)
	}
	if get(userCookie)["capabilities"].(contractObject)["canVote"] != false {
		t.Fatal("disabled vote offered")
	}
	_, reply, err := smokeSrv.st.CreateReply(ctx, th.ID, 2, "user", "answer", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	postPath := fmt.Sprintf("/api/v1/posts/%d", reply.ID)
	post := func() contractObject {
		return assertContractResponse(t, doc, "GET", "/api/v1/posts/{pid}", smokeGet(t, postPath, cookie), 200)["data"].(contractObject)
	}
	if get(cookie)["capabilities"].(contractObject)["canCancelBounty"] != false || post()["capabilities"].(contractObject)["canAccept"] != true {
		t.Fatal("reply state not reflected")
	}
	flowSQL(t, `UPDATE thread_bounties SET closes_at=now()-interval '1 second' WHERE thread_id=$1`, th.ID)
	if post()["capabilities"].(contractObject)["canAccept"] != false || get(cookie)["capabilities"].(contractObject)["canCancelBounty"] != true {
		t.Fatal("expiry not reflected")
	}
	flowSQL(t, `UPDATE thread_bounties SET closes_at=now()+interval '1 day' WHERE thread_id=$1`, th.ID)
	if err = smokeSrv.st.SetAcceptedReply(ctx, th.ID, reply.ID, owner.ID, true); err != nil {
		t.Fatal(err)
	}
	paid := post()
	if paid["accepted"] != true || paid["capabilities"].(contractObject)["canUnaccept"] != false {
		t.Fatal("paid bounty offers undo", paid)
	}
	list := assertContractResponse(t, doc, "GET", "/api/v1/threads/{tid}/posts", smokeGet(t, path+"/posts", cookie), 200)["data"].([]any)
	for _, raw := range list {
		p := raw.(contractObject)
		if p["capabilities"].(contractObject)["canUnaccept"] != false {
			t.Fatal("list offers paid undo", p)
		}
	}
	mc.Forums = append(mc.Forums, store.ForumMembership{ForumID: 1, MinimumLevel: 4, MembersOnly: true})
	setMemberAPIConfig(t, mc)
	checkJSON(t, smokeGet(t, path, cookie), 404)
}
