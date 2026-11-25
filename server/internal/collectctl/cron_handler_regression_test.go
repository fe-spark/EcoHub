package collectctl

import (
	"net/http"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"server/internal/collect"
)

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
		collect.RemoveCronByTaskId("task_partial_1")
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
	if err := collect.SaveFilmTask(orig); err != nil {
		t.Fatal(err)
	}

	// 仅提交 id、spec、remark（不传 state 和 time），不应覆盖原有 state=true 与 time=24
	c, w := testJSONContext(http.MethodPost, "/api/manage/cron/save", `{"id":"task_partial_1","spec":"0 30 * * * *","remark":"新备注"}`)
	CronHd.FilmCronSave(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	updated, err := collect.GetFilmTaskById("task_partial_1")
	if err != nil {
		t.Fatal(err)
	}
	if !updated.State || updated.Time != 24 || updated.Spec != "0 30 * * * *" || updated.Remark != "新备注" {
		t.Fatalf("unexpected task after partial update: %+v", updated)
	}
}

func TestCronHandler_FilmCronSave_RejectsInvalidModelAndTime(t *testing.T) {
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
		collect.RemoveCronByTaskId("task_invalid_1")
		db.Mdb = prev
	})

	orig := model.FilmCollectTask{
		Id:    "task_invalid_1",
		Spec:  "0 0 * * * *",
		Model: model.TaskModelAutoCollect,
		Time:  24,
		State: true,
	}
	if err := collect.SaveFilmTask(orig); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		body string
	}{
		{"unsupported_model", `{"id":"task_invalid_1","model":99}`},
		{"zero_time", `{"id":"task_invalid_1","time":0}`},
		{"negative_time_not_full", `{"id":"task_invalid_1","time":-2}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := testJSONContext(http.MethodPost, "/api/manage/cron/save", tc.body)
			CronHd.FilmCronSave(c)
			resp := decodeResponse(t, w)
			if resp.Code == dto.SUCCESS {
				t.Fatalf("%s 应被校验拒绝，却返回 SUCCESS", tc.name)
			}
		})
	}

	after, err := collect.GetFilmTaskById("task_invalid_1")
	if err != nil {
		t.Fatal(err)
	}
	if after.Model != model.TaskModelAutoCollect || after.Time != 24 {
		t.Fatalf("非法参数不应修改已存任务，got %+v", after)
	}
}

func TestCronHandler_StateAndRun_Validation(t *testing.T) {
	// 1. 空 ids 变更状态 -> Failed
	c, w := testJSONContext(http.MethodPost, "/api/manage/cron/state", `{"ids":[],"state":true}`)
	CronHd.ChangeTaskState(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids for ChangeTaskState, got %d", resp.Code)
	}

	// 2. 空 ids 执行任务 -> Failed
	c, w = testJSONContext(http.MethodPost, "/api/manage/cron/run", `{"ids":[]}`)
	CronHd.RunFilmCronTask(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids for RunFilmCronTask, got %d", resp.Code)
	}
}
