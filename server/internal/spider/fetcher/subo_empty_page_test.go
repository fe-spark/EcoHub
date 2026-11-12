package fetcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"server/internal/model"
	"server/internal/spider/converter"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

// TestSuboRealScenario_EmptyPlayListHandledAsSuccess 模拟速博第 5842 页（正常数据）与第 5843 页（无播放链接数据）
func TestSuboRealScenario_EmptyPlayListHandledAsSuccess(t *testing.T) {
	// 构造 Mock HTTP 采集站，分别响应第 5842 页和第 5843 页
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pg := r.URL.Query().Get("pg")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if pg == "5842" {
			// 模拟 5842 页：部分有有效播放链接
			resp := map[string]interface{}{
				"code":      1,
				"msg":       "数据列表",
				"page":      5842,
				"pagecount": 5871,
				"limit":     "20",
				"total":     117413,
				"list": []map[string]interface{}{
					{
						"vod_id":        2718,
						"vod_name":      "偷香",
						"vod_play_from": "subyun$$$subm3u8",
						"vod_play_url":  "正片$https://example.com/play1.m3u8",
					},
					{
						"vod_id":        2719,
						"vod_name":      "无效影片",
						"vod_play_from": "subyun$$$subm3u8",
						"vod_play_url":  "$$$",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if pg == "5843" {
			// 模拟 5843 页：全部无播放链接（如速博真实情况）
			resp := map[string]interface{}{
				"code":      1,
				"msg":       "数据列表",
				"page":      5843,
				"pagecount": 5871,
				"limit":     "20",
				"total":     117413,
				"list": []map[string]interface{}{
					{
						"vod_id":        98079,
						"vod_name":      "NBA常规赛-勇士vs尼克斯-20221221",
						"vod_play_from": "subyun$$$subm3u8",
						"vod_play_url":  "$$$",
					},
					{
						"vod_id":        98080,
						"vod_name":      "NBA常规赛-奇才vs太阳-20221221",
						"vod_play_from": "subyun$$$subm3u8",
						"vod_play_url":  "$$$",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	oldDeps := deps
	t.Cleanup(func() { deps = oldDeps })

	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		return func(error) {}, nil
	}

	// 使用实际的业务解析与过滤流程模拟真实 GetFilmDetail
	deps.GetFilmDetail = func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
		r.Params.Set("ac", "detail")
		utils.ApiGet(&r)
		if len(r.Resp) == 0 {
			return nil, errors.New("empty response body")
		}
		var pageData model.FilmDetailLPage
		if err := json.Unmarshal(r.Resp, &pageData); err != nil {
			return nil, err
		}
		return converter.ConvertFilmDetails(pageData.List), nil
	}

	savedPages := make(map[int]int)
	deps.SavePage = func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
		savedPages[page] = len(list)
		return scheduler.Mids{}, nil
	}

	source := &model.FilmSource{
		Id:   "subo_mock",
		Name: "速博(SUBO)",
		Uri:  server.URL,
	}

	// 执行两页的目标抓取：5842 和 5843
	hasWrites, succ, fail, failedPages, err := CollectPages(context.Background(), 5871, []int{5842, 5843}, source, 24, nil)
	if err != nil {
		t.Fatalf("采集不应报错，实际错误: %v", err)
	}
	if !hasWrites {
		t.Fatal("hasWrites 应为 true")
	}
	if succ != 2 {
		t.Fatalf("成功页数期望为 2，实际为: %d", succ)
	}
	if fail != 0 {
		t.Fatalf("失败页数期望为 0，实际为: %d", fail)
	}
	if len(failedPages) != 0 {
		t.Fatalf("失败页码列表应为空，实际为: %v", failedPages)
	}

	// 验证 5842 页写入了 1 条有效影片，5843 页写入了 0 条有效影片
	if savedPages[5842] != 1 {
		t.Fatalf("5842 页期望写入 1 条，实际写入: %d", savedPages[5842])
	}
	if savedPages[5843] != 0 {
		t.Fatalf("5843 页期望写入 0 条，实际写入: %d", savedPages[5843])
	}
}

// TestSuboRealScenario_MixedResultsWithRealError 验证包含正常页、空数据页、真实网络超时的混合场景
func TestSuboRealScenario_MixedResultsWithRealError(t *testing.T) {
	oldDeps := deps
	oldDelay := fetchRetryBase
	t.Cleanup(func() {
		deps = oldDeps
		fetchRetryBase = oldDelay
	})
	fetchRetryBase = 0

	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		return func(error) {}, nil
	}

	deps.GetFilmDetail = func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
		pg := r.Params.Get("pg")
		switch pg {
		case "1":
			return []model.MovieDetail{{Id: 1, Name: "正常影片"}}, nil
		case "2":
			// 过滤后无有效影片（空列表）
			return []model.MovieDetail{}, nil
		case "3":
			// 连接被重置会先补抓，次数用完仍失败，这一页记失败。
			return nil, errors.New("connection reset by peer")
		default:
			return nil, errors.New("unknown page")
		}
	}

	deps.SavePage = func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
		return scheduler.Mids{}, nil
	}

	source := &model.FilmSource{
		Id:   "subo_mixed",
		Name: "速博混合测试",
	}

	hasWrites, succ, fail, failedPages, err := CollectPages(context.Background(), 3, []int{1, 2, 3}, source, 24, nil)
	// 第 3 页失败，因此 CollectPages 应该返回包含第 3 页失败的错误
	if err == nil {
		t.Fatal("第 3 页存在真实网络错误，整体应当报告失败")
	}
	if !hasWrites {
		t.Fatal("有成功页（第 1 页和第 2 页），hasWrites 应为 true")
	}
	if succ != 2 {
		t.Fatalf("成功页数期望为 2（第 1 页与第 2 页），实际为: %d", succ)
	}
	if fail != 1 {
		t.Fatalf("失败页数期望为 1（第 3 页），实际为: %d", fail)
	}
	if len(failedPages) != 1 || failedPages[0] != 3 {
		t.Fatalf("失败列表期望为 [3]，实际为: %v", failedPages)
	}
}
