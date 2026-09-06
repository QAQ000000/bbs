// SPDX-License-Identifier: AGPL-3.0-or-later

// store 集成测试：对独立测试库（forum_test）验证关键写路径的计数一致性。
// 默认 DSN 可用 FORUM_TEST_DSN 覆盖；数据库不可达时跳过。

package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/jackc/pgx/v5/pgxpool"

	"dzforum/internal/db"
)

var testStore *Store
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := os.Getenv("FORUM_TEST_DSN")
	if dsn == "" {
		fmt.Println("SKIP: store integration tests require explicit FORUM_TEST_DSN")
		os.Exit(0)
	}
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(parsed.ConnConfig.Database, "gobbs_test_") {
		fmt.Println("FORUM_TEST_DSN must name a disposable gobbs_test_ database")
		os.Exit(1)
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		fmt.Println("SKIP: 测试数据库不可达（", err, "）")
		os.Exit(1)
	}
	testPool = pool
	// 全量重建 schema
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		fmt.Println("SKIP: 无法重置测试库:", err)
		os.Exit(1)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		fmt.Println("FATAL: 迁移失败:", err)
		os.Exit(1)
	}
	testStore = New(pool)
	code := m.Run()
	testPool.Close()
	os.Exit(code)
}

// setupUsers 建两个测试用户，返回 id。
func setupUsers(t *testing.T) (int64, int64) {
	t.Helper()
	u1, err := testStore.CreateUser(context.Background(), "作者"+t.Name(), "pass123456", "")
	if err != nil {
		t.Fatal(err)
	}
	u2, err := testStore.CreateUser(context.Background(), "回复者"+t.Name(), "pass123456", "")
	if err != nil {
		t.Fatal(err)
	}
	return u1.ID, u2.ID
}

func setupForum(t *testing.T) int64 {
	t.Helper()
	var fid int64
	if err := testStore.pool.QueryRow(context.Background(),
		`INSERT INTO categories (name) VALUES ('测试分类') RETURNING id`).Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	if err := testStore.pool.QueryRow(context.Background(),
		`INSERT INTO forums (category_id, name) VALUES (1,'测试版块') RETURNING id`).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	return fid
}

func TestThreadCounters(t *testing.T) {
	ctx := context.Background()
	author, replier := setupUsers(t)
	fid := setupForum(t)

	th, p, err := testStore.CreateThread(ctx, fid, author, "作者", "计数主题", "首楼 :smile:", "<p>首楼</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if th.PostCount != 1 {
		t.Fatalf("新主题 post_count 应为 1: %d", th.PostCount)
	}
	if p.Floor != 1 || p.Version != 1 {
		t.Fatalf("首楼 floor/version: %d/%d", p.Floor, p.Version)
	}

	// 两条回复：楼层号递增、post_count 一致
	for i := 0; i < 2; i++ {
		th2, p2, err := testStore.CreateReply(ctx, th.ID, replier, "回复者", fmt.Sprintf("回复%d", i), "<p>r</p>", false, "")
		if err != nil {
			t.Fatal(err)
		}
		if p2.Floor != i+2 || th2.PostCount != i+2 {
			t.Fatalf("回复%d: floor=%d post_count=%d", i, p2.Floor, th2.PostCount)
		}
	}

	// 版块计数
	var tc, pc int64
	if err := testStore.pool.QueryRow(ctx,
		`SELECT thread_count, post_count FROM forums WHERE id=$1`, fid).Scan(&tc, &pc); err != nil {
		t.Fatal(err)
	}
	if tc != 1 || pc != 3 {
		t.Fatalf("版块计数: thread=%d post=%d，期望 1/3", tc, pc)
	}
	// 读取路径口径断言（能抓住列顺序错位类回归）：
	// 版块行 ThreadCount/PostCount/TodayCount 必须与真实公开内容一致
	cats, err := testStore.CategoriesWithForums(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, c := range cats {
		for _, f := range c.Forums {
			if f.ID == fid {
				seen = true
				if f.ThreadCount != 1 || f.PostCount != 3 {
					t.Fatalf("读取路径版块计数错位: thread=%d post=%d，期望 1/3", f.ThreadCount, f.PostCount)
				}
				if f.TodayCount != 3 {
					t.Fatalf("实时今日帖数应为 3: %d", f.TodayCount)
				}
			}
		}
	}
	if !seen {
		t.Fatal("版块行未返回")
	}

	// 编辑：版本号自增
	p1, _ := testStore.Post(ctx, p.ID)
	_, thUp, err := testStore.UpdatePost(ctx, p.ID, 0, 0, "", "计数主题（改）", "改后内容", "<p>改</p>")
	if err != nil {
		t.Fatal(err)
	}
	if thUp.Title != "计数主题（改）" {
		t.Fatalf("首楼编辑应同步主题标题: %s", thUp.Title)
	}
	pAfter, _ := testStore.Post(ctx, p1.ID)
	if pAfter.Version != 2 {
		t.Fatalf("编辑后版本应为 2: %d", pAfter.Version)
	}

	// 删除一条回复：主题与作者计数同步
	posts, _ := testStore.Posts(ctx, th.ID, 1, 10, true)
	if _, _, err := testStore.DeletePost(ctx, posts[2].ID); err != nil {
		t.Fatal(err)
	}
	th3, _ := testStore.Thread(ctx, th.ID)
	if th3.PostCount != 2 {
		t.Fatalf("删回复后 post_count 应为 2: %d", th3.PostCount)
	}
	var authorCount int64
	if err := testStore.pool.QueryRow(ctx,
		`SELECT post_count FROM users WHERE id=$1`, replier).Scan(&authorCount); err != nil {
		t.Fatal(err)
	}
	if authorCount != 1 {
		t.Fatalf("回复者 post_count 应回补为 1: %d", authorCount)
	}
}

func TestPrunePostsRecompute(t *testing.T) {
	ctx := context.Background()
	author, replier := setupUsers(t)
	fid := setupForum(t)

	th, _, err := testStore.CreateThread(ctx, fid, author, "作者", "批量删帖", "首楼", "<p>x</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := testStore.CreateReply(ctx, th.ID, replier, "回复者", "待删", "<p>y</p>", false, ""); err != nil {
			t.Fatal(err)
		}
	}
	// 无条件拒绝
	if _, err := testStore.PrunePosts(ctx, "", 0, 0, nil); err == nil {
		t.Fatal("无条件批量删除应被拒绝")
	}
	n, err := testStore.PrunePosts(ctx, "回复者"+t.Name(), 0, 0, nil)
	if err != nil || n != 3 {
		t.Fatalf("应删 3 条: n=%d err=%v", n, err)
	}
	// 计数一致
	th2, _ := testStore.Thread(ctx, th.ID)
	if th2.PostCount != 1 {
		t.Fatalf("批量删后 post_count 应为 1: %d", th2.PostCount)
	}
	var pc int64
	if err := testStore.pool.QueryRow(ctx, `SELECT post_count FROM forums WHERE id=$1`, fid).Scan(&pc); err != nil {
		t.Fatal(err)
	}
	if pc != 1 {
		t.Fatalf("版块 post_count 应为 1: %d", pc)
	}
}

func TestLikeToggleAndRead(t *testing.T) {
	ctx := context.Background()
	author, reader := setupUsers(t)
	fid := setupForum(t)

	th, p, err := testStore.CreateThread(ctx, fid, author, "作者", "点赞主题", "内容", "<p>c</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	// 赞 → 取消
	liked, count, err := testStore.LikeToggle(ctx, p.ID, reader)
	if err != nil || !liked || count != 1 {
		t.Fatalf("首次点赞: liked=%v count=%d err=%v", liked, count, err)
	}
	liked, count, err = testStore.LikeToggle(ctx, p.ID, reader)
	if err != nil || liked || count != 0 {
		t.Fatalf("取消点赞: liked=%v count=%d err=%v", liked, count, err)
	}
	// Reading metrics alone do not qualify for experience-based membership.
	if _, err := testStore.pool.Exec(ctx,
		`UPDATE users SET days_visited=3, posts_read=18 WHERE id=$1`, reader); err != nil {
		t.Fatal(err)
	}
	reply, err := func() (*Post, error) {
		_, p, e := testStore.CreateReply(ctx, th.ID, author, "作者", "reply", "", false, "")
		return p, e
	}()
	if err != nil {
		t.Fatal(err)
	}
	for _, post := range []*Post{p, p, reply} {
		if err := testStore.RecordMemberRead(ctx, reader, post.ID, th.ID, post.Floor); err != nil {
			t.Fatal(err)
		}
	}
	if err := testStore.UpgradeMember(ctx, reader); err != nil {
		t.Fatal(err)
	}
	var levelID int
	var reads int64
	if err := testStore.pool.QueryRow(ctx,
		`SELECT ms.level_id, u.posts_read FROM member_states ms JOIN users u ON u.id=ms.user_id WHERE u.id=$1`, reader).Scan(&levelID, &reads); err != nil {
		t.Fatal(err)
	}
	if reads != 20 || levelID != 0 {
		t.Fatalf("reading=%d level=%d, expected 20/0 without experience", reads, levelID)
	}
}

func TestSearch(t *testing.T) {
	ctx := context.Background()
	author, _ := setupUsers(t)
	fid := setupForum(t)

	th, p, err := testStore.CreateThread(ctx, fid, author, "作者", "搜索引擎测试主题", "这是关于稀疏索引与倒排的内容", "<p>x</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = th
	// 清空索引后重建（验证存量补齐路径）
	if _, err := testPool.Exec(ctx, `UPDATE posts SET search_data=NULL`); err != nil {
		t.Fatal(err)
	}
	if n, err := testStore.ReindexSearch(ctx); err != nil || n == 0 {
		t.Fatalf("重建索引: n=%d err=%v", n, err)
	}
	// 中文命中（bigram）
	hits, total, err := testStore.Search(ctx, "搜索引擎", 1, 10, SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 || len(hits) == 0 || hits[0].ThreadID != th.ID {
		t.Fatalf("中文搜索未命中: total=%d", total)
	}
	// 英文命中
	hits, _, err = testStore.Search(ctx, "倒排", 1, 10, SearchOpts{})
	if err != nil || len(hits) == 0 {
		t.Fatalf("命中查询失败: %v", err)
	}
	// 无关词
	_, total, _ = testStore.Search(ctx, "完全不相关的词组", 1, 10, SearchOpts{})
	if total != 0 {
		t.Fatalf("无关词不应命中: %d", total)
	}
	_ = p
}

func TestSetPostApprovedCounters(t *testing.T) {
	ctx := context.Background()
	author, replier := setupUsers(t)
	fid := setupForum(t)
	th, _, err := testStore.CreateThread(ctx, fid, author, "作者", "审批回复", "首楼", "<p>x</p>", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, p, err := testStore.CreateReply(ctx, th.ID, replier, "回复者", "待审回复", "<p>y</p>", true, "manual")
	if err != nil {
		t.Fatal(err)
	}
	var pc, upc int64
	if err := testStore.pool.QueryRow(ctx, `SELECT post_count FROM forums WHERE id=$1`, fid).Scan(&pc); err != nil {
		t.Fatal(err)
	}
	if err := testStore.pool.QueryRow(ctx, `SELECT post_count FROM users WHERE id=$1`, replier).Scan(&upc); err != nil {
		t.Fatal(err)
	}
	if pc != 1 || upc != 0 {
		t.Fatalf("待审回复不应进入公开口径: forum=%d user=%d", pc, upc)
	}
	p2, th2, err := testStore.SetPostApproved(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Pending {
		t.Fatal("过审后楼层仍 pending")
	}
	if err := testStore.pool.QueryRow(ctx, `SELECT post_count FROM forums WHERE id=$1`, fid).Scan(&pc); err != nil {
		t.Fatal(err)
	}
	if err := testStore.pool.QueryRow(ctx, `SELECT post_count FROM users WHERE id=$1`, replier).Scan(&upc); err != nil {
		t.Fatal(err)
	}
	if pc != 2 || upc != 1 {
		t.Fatalf("过审后公开口径应为 forum=2 user=1，得到 %d/%d", pc, upc)
	}
	if th2.LastPostUID != replier {
		t.Fatalf("主题 last_post_uid 应为回复者: %d", th2.LastPostUID)
	}
}

func TestModeratorRelationTable(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	fid := setupForum(t)
	u, err := testStore.UserByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.SaveForum(ctx, fid, 1, "测试版块", "", u.Username); err != nil {
		t.Fatal(err)
	}
	ids, err := testStore.ModeratorForumIDs(ctx, uid)
	if err != nil || len(ids) != 1 || ids[0] != fid {
		t.Fatalf("关系表未写入: ids=%v err=%v", ids, err)
	}
	// 改名后仍按 user_id 命中
	if _, err := testStore.pool.Exec(ctx, `UPDATE users SET username=$2 WHERE id=$1`, uid, u.Username+"改名"); err != nil {
		t.Fatal(err)
	}
	ids, err = testStore.ModeratorForumIDs(ctx, uid)
	if err != nil || len(ids) != 1 || ids[0] != fid {
		t.Fatalf("改名后丢权: ids=%v err=%v", ids, err)
	}
}

// TestChangePasswordClearsFlag 改密成功后必须改密标志清除（-seed 强制改密闭环）。
func TestChangePasswordClearsFlag(t *testing.T) {
	ctx := context.Background()
	author, _ := setupUsers(t)
	if _, err := testStore.pool.Exec(ctx,
		`UPDATE users SET must_change_password=true WHERE id=$1`, author); err != nil {
		t.Fatal(err)
	}
	if err := testStore.ChangePassword(ctx, author, "pass123456", "newpass12345", ""); err != nil {
		t.Fatal(err)
	}
	var mc bool
	var hash string
	if err := testStore.pool.QueryRow(ctx,
		`SELECT must_change_password, password_hash FROM users WHERE id=$1`, author).Scan(&mc, &hash); err != nil {
		t.Fatal(err)
	}
	if mc {
		t.Fatal("改密后 must_change_password 应清除")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("newpass12345")) != nil {
		t.Fatal("新密码哈希不匹配")
	}
}
