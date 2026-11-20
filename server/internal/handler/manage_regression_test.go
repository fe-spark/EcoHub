package handler

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/spider"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUserHandler_LoginAccountLimiterAndUnifiedError(t *testing.T) {
	setupManageConfigDB(t)
	loginFailMu.Lock()
	loginFailState = map[string]loginAttemptState{}
	loginFailMu.Unlock()

	for i := 0; i < 5; i++ {
		c, w := testJSONContext(http.MethodPost, "/api/login", `{"userName":"nobody_user","password":"bad"}`)
		c.Request.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", i+1))
		UserHd.Login(c)
		resp := decodeResponse(t, w)
		if resp.Code != dto.FAILED || resp.Msg != "用户名或密码错误" {
			t.Fatalf("attempt %d: got code=%d msg=%q, want 用户名或密码错误", i+1, resp.Code, resp.Msg)
		}
	}

	// 即使换第 6 个 IP，账号维度限流仍应生效
	c6, w6 := testJSONContext(http.MethodPost, "/api/login", `{"userName":"nobody_user","password":"bad"}`)
	c6.Request.Header.Set("X-Forwarded-For", "10.0.0.99")
	UserHd.Login(c6)
	resp6 := decodeResponse(t, w6)
	if resp6.Code != dto.FAILED || !strings.Contains(resp6.Msg, "频繁") {
		t.Fatalf("expected account rate limit on 6th attempt, got code=%d msg=%q", resp6.Code, resp6.Msg)
	}
}

func TestReserveLoginAttempts_ParallelBurstStopsAtFive(t *testing.T) {
	loginFailMu.Lock()
	loginFailState = map[string]loginAttemptState{}
	loginFailMu.Unlock()

	var wg sync.WaitGroup
	var allowed atomic.Int32
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if reserveLoginAttempts("acct|burst") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 5 {
		t.Fatalf("parallel burst allowed %d, want 5", got)
	}
}

func TestCronHandler_FilmCronSave_PartialUpdatePreservesStateAndTime(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.CrontabRecord{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() {
		spider.RemoveCronByTaskId("task_partial_1")
		db.Mdb = prev
	})

	orig := model.FilmCollectTask{
		Id:     "task_partial_1",
		Spec:   "0 0 * * * *",
		Model:  model.TaskModelAutoCollect,
		Time:   24,
		State:  true,
		Remark: "旧备注",
	}
	if err := repository.SaveFilmTask(orig); err != nil {
		t.Fatal(err)
	}

	// 仅提交 id、spec、remark（不传 state 和 time），不应覆盖原有 state=true 与 time=24
	c, w := testJSONContext(http.MethodPost, "/api/manage/cron/save", `{"id":"task_partial_1","spec":"0 30 * * * *","remark":"新备注"}`)
	CronHd.FilmCronSave(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	updated, err := repository.GetFilmTaskById("task_partial_1")
	if err != nil {
		t.Fatal(err)
	}
	if !updated.State || updated.Time != 24 || updated.Spec != "0 30 * * * *" || updated.Remark != "新备注" {
		t.Fatalf("unexpected task after partial update: %+v", updated)
	}
}
