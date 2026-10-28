package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/service"
	"server/internal/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func TestTMDBHandler_Scrape_Validation(t *testing.T) {
	// 1. 无效 JSON
	c, w := testJSONContext(http.MethodPost, "/api/manage/tmdb/scrape", "invalid-json")
	TMDBHd.Scrape(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}

	// 2. 缺少 ids 参数
	c, w = testJSONContext(http.MethodPost, "/api/manage/tmdb/scrape", `{"ids":[]}`)
	TMDBHd.Scrape(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}

func TestFilmHandler_FilmDelete_Validation(t *testing.T) {
	// 1. 无效 JSON
	c, w := testJSONContext(http.MethodPost, "/api/manage/film/delete", "invalid-json")
	FilmHd.FilmDelete(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}

	// 2. 缺少 ids 参数
	c, w = testJSONContext(http.MethodPost, "/api/manage/film/delete", `{"ids":[]}`)
	FilmHd.FilmDelete(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}

func TestFileHandler_DelFile_Validation(t *testing.T) {
	// 1. 无效 JSON
	c, w := testJSONContext(http.MethodPost, "/api/manage/file/delete", "invalid-json")
	FileHd.DelFile(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}

	// 2. 缺少 ids 参数
	c, w = testJSONContext(http.MethodPost, "/api/manage/file/delete", `{"ids":[]}`)
	FileHd.DelFile(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}

func TestFilmHandler_FilmRefresh_Validation(t *testing.T) {
	// 1. 无效 JSON
	c, w := testJSONContext(http.MethodPost, "/api/manage/film/refresh", "invalid-json")
	FilmHd.FilmRefresh(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}

	// 2. 缺少 ids 参数
	c, w = testJSONContext(http.MethodPost, "/api/manage/film/refresh", `{"ids":[]}`)
	FilmHd.FilmRefresh(c)
	resp = decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}

func TestTMDBHandler_GetConfig_HidesApiKeyForNonAdmin(t *testing.T) {
	setupManageConfigDB(t)
	_ = service.TMDBSvc.UpdateConfig(model.TMDBConfig{
		Enabled: true,
		ApiKey:  "secret-tmdb-key-12345",
	})
	cases := []struct {
		name       string
		claims     any
		wantApiKey string
	}{
		{name: "anonymous", claims: nil, wantApiKey: ""},
		{
			name:       "visitor",
			claims:     &utils.UserClaims{UserID: 10002, UserName: "guest", Role: model.UserRoleVisitor},
			wantApiKey: "",
		},
		{
			name:       "normal user",
			claims:     &utils.UserClaims{UserID: 10003, UserName: "normal", Role: model.UserRoleNormal},
			wantApiKey: "",
		},
		{
			name:       "admin",
			claims:     &utils.UserClaims{UserID: config.UserIdInitialVal, UserName: "admin", Role: model.UserRoleAdmin},
			wantApiKey: repository.MaskTMDBApiKey("secret-tmdb-key-12345"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := testContext(http.MethodGet, "/api/manage/tmdb/config")
			if tc.claims != nil {
				c.Set(config.AuthUserClaims, tc.claims)
			}
			TMDBHd.GetConfig(c)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d want 200", w.Code)
			}
			resp := decodeResponse(t, w)
			if resp.Code != dto.SUCCESS {
				t.Fatalf("code=%d want %d", resp.Code, dto.SUCCESS)
			}
			raw, _ := json.Marshal(resp.Data)
			var cfg model.TMDBConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if cfg.ApiKey != tc.wantApiKey {
				t.Fatalf("expected apiKey %q, got %q", tc.wantApiKey, cfg.ApiKey)
			}
		})
	}
}

func TestUserHandler_UserProfile(t *testing.T) {
	setupManageConfigDB(t)
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	origRdb := db.Rdb
	db.Rdb = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() {
		_ = db.Rdb.Close()
		db.Rdb = origRdb
	}()
	u := model.User{
		Model:    gorm.Model{ID: 10005},
		UserName: "testuser",
		NickName: "OldNick",
		Role:     model.UserRoleNormal,
		Status:   0,
	}
	db.Mdb.Create(&u)

	newNick := "NewNick"
	newPwd := "newpassword123"
	payload, _ := json.Marshal(model.UserUpdatePayload{
		NickName: &newNick,
		Password: &newPwd,
	})
	c, w := testContext(http.MethodPost, "/api/manage/user/profile")
	c.Set(config.AuthUserClaims, &utils.UserClaims{
		UserID:   10005,
		UserName: "testuser",
		Role:     model.UserRoleNormal,
	})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/user/profile", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")

	UserHd.UserProfile(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var updated model.User
	db.Mdb.First(&updated, 10005)
	if updated.NickName != "NewNick" {
		t.Fatalf("expected NewNick, got %s", updated.NickName)
	}

	// 2. 存在旧密码时，未传 oldPassword 应被拦截
	newerPwd := "newerpwd456"
	payloadNoOld, _ := json.Marshal(model.UserUpdatePayload{Password: &newerPwd})
	cNoOld, wNoOld := testContext(http.MethodPost, "/api/manage/user/profile")
	cNoOld.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10005, Role: model.UserRoleNormal})
	cNoOld.Request = httptest.NewRequest(http.MethodPost, "/api/manage/user/profile", bytes.NewReader(payloadNoOld))
	cNoOld.Request.Header.Set("Content-Type", "application/json")
	UserHd.UserProfile(cNoOld)
	respNoOld := decodeResponse(t, wNoOld)
	if respNoOld.Code != dto.FAILED || !strings.Contains(respNoOld.Msg, "必须提供原密码") {
		t.Fatalf("expected failure on missing old password, got code=%d msg=%s", respNoOld.Code, respNoOld.Msg)
	}

	// 3. 传入错误的 oldPassword 应被拦截
	wrongOld := "wrongpassword"
	payloadWrongOld, _ := json.Marshal(model.UserUpdatePayload{Password: &newerPwd, OldPassword: &wrongOld})
	cWrong, wWrong := testContext(http.MethodPost, "/api/manage/user/profile")
	cWrong.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10005, Role: model.UserRoleNormal})
	cWrong.Request = httptest.NewRequest(http.MethodPost, "/api/manage/user/profile", bytes.NewReader(payloadWrongOld))
	cWrong.Request.Header.Set("Content-Type", "application/json")
	UserHd.UserProfile(cWrong)
	respWrong := decodeResponse(t, wWrong)
	if respWrong.Code != dto.FAILED || !strings.Contains(respWrong.Msg, "原密码校验失败") {
		t.Fatalf("expected failure on wrong old password, got code=%d msg=%s", respWrong.Code, respWrong.Msg)
	}

	// 4. 传入正确的 oldPassword 成功更新
	correctOld := "newpassword123"
	payloadCorrect, _ := json.Marshal(model.UserUpdatePayload{Password: &newerPwd, OldPassword: &correctOld})
	cCorrect, wCorrect := testContext(http.MethodPost, "/api/manage/user/profile")
	cCorrect.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10005, Role: model.UserRoleNormal})
	cCorrect.Request = httptest.NewRequest(http.MethodPost, "/api/manage/user/profile", bytes.NewReader(payloadCorrect))
	cCorrect.Request.Header.Set("Content-Type", "application/json")
	UserHd.UserProfile(cCorrect)
	respCorrect := decodeResponse(t, wCorrect)
	if respCorrect.Code != dto.SUCCESS {
		t.Fatalf("expected success on correct old password, got code=%d msg=%s", respCorrect.Code, respCorrect.Msg)
	}
}

func TestFileHandler_OwnershipEnforcement(t *testing.T) {
	setupManageConfigDB(t)
	file := model.FileInfo{
		Model: gorm.Model{ID: 99},
		Uid:   10001,
		Name:  "UserFile",
	}
	db.Mdb.Create(&file)

	c, w := testJSONContext(http.MethodPost, "/api/manage/file/rename", `{"id":99,"name":"HackedName"}`)
	c.Set(config.AuthUserClaims, &utils.UserClaims{
		UserID: 10002,
		Role:   model.UserRoleNormal,
	})
	FileHd.RenameFile(c)
	resp := decodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on non-owner rename, got %d", resp.Code)
	}

	c2, w2 := testJSONContext(http.MethodPost, "/api/manage/file/delete", `{"ids":[99]}`)
	c2.Set(config.AuthUserClaims, &utils.UserClaims{
		UserID: 10002,
		Role:   model.UserRoleNormal,
	})
	FileHd.DelFile(c2)
	resp2 := decodeResponse(t, w2)
	if resp2.Code != dto.FAILED {
		t.Fatalf("expected FAILED on non-owner delete, got %d", resp2.Code)
	}

	c3, w3 := testJSONContext(http.MethodPost, "/api/manage/file/rename", `{"id":99,"name":"UpdatedName"}`)
	c3.Set(config.AuthUserClaims, &utils.UserClaims{
		UserID: 10001,
		Role:   model.UserRoleNormal,
	})
	FileHd.RenameFile(c3)
	resp3 := decodeResponse(t, w3)
	if resp3.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS on owner rename, got %d msg=%s", resp3.Code, resp3.Msg)
	}

	c4, w4 := testJSONContext(http.MethodPost, "/api/manage/file/delete", `{"ids":[99]}`)
	c4.Set(config.AuthUserClaims, &utils.UserClaims{
		UserID: config.UserIdInitialVal,
		Role:   model.UserRoleAdmin,
	})
	FileHd.DelFile(c4)
	resp4 := decodeResponse(t, w4)
	if resp4.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS on admin delete, got %d msg=%s", resp4.Code, resp4.Msg)
	}
	// 验证批量删除原子预检：混入无权删除的文件时整批拒绝，本人文件不被删除
	fileOwn := model.FileInfo{Model: gorm.Model{ID: 101}, Uid: 10001, Name: "OwnFile"}
	fileOther := model.FileInfo{Model: gorm.Model{ID: 102}, Uid: 10002, Name: "OtherFile"}
	db.Mdb.Create(&fileOwn)
	db.Mdb.Create(&fileOther)

	cBatch, wBatch := testJSONContext(http.MethodPost, "/api/manage/file/delete", `{"ids":[101, 102]}`)
	cBatch.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10001, Role: model.UserRoleNormal})
	FileHd.DelFile(cBatch)
	respBatch := decodeResponse(t, wBatch)
	if respBatch.Code != dto.FAILED {
		t.Fatalf("expected FAILED on batch with unauthorized item, got %d", respBatch.Code)
	}
	// 验证 101 未被删除
	var checkOwn model.FileInfo
	if err := db.Mdb.First(&checkOwn, 101).Error; err != nil {
		t.Fatalf("expected file 101 to remain intact after batch failure, got err: %v", err)
	}
}
