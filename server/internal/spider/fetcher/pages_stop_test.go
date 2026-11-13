package fetcher

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"server/internal/model"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

func TestCollectPages_WithTargetPages(t *testing.T) {
	oldDeps := deps
	t.Cleanup(func() { deps = oldDeps })

	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		return func(error) {}, nil
	}
	deps.GetFilmDetail = func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
		pg, _ := strconv.Atoi(r.Params.Get("pg"))
		if pg == 3190 {
			return nil, errors.New("timeout")
		}
		return []model.MovieDetail{{Id: int64(pg), Name: "测试"}}, nil
	}
	deps.SavePage = func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
		return scheduler.Mids{Affected: []int64{int64(page)}}, nil
	}

	source := &model.FilmSource{Id: "test_targets", Name: "目标源"}
	hasWrites, succ, fail, failedPages, err := CollectPages(context.Background(), 10000, []int{3190, 4036}, source, 24, nil)
	if !hasWrites {
		t.Fatal("expected hasWrites=true")
	}
	if succ != 1 || fail != 1 {
		t.Fatalf("expected succ=1, fail=1, got succ=%d, fail=%d", succ, fail)
	}
	if len(failedPages) != 1 || failedPages[0] != 3190 {
		t.Fatalf("expected failedPages=[3190], got %v", failedPages)
	}
	if err == nil || !strings.Contains(err.Error(), "3190") {
		t.Fatalf("expected error mentioning 3190, got: %v", err)
	}
}

func TestCollectPages_EmptyListPageTreatedAsSuccess(t *testing.T) {
	oldDeps := deps
	t.Cleanup(func() { deps = oldDeps })

	deps.WaitTurn = func(ctx context.Context, _ *model.FilmSource, _ string) (func(error), error) {
		return func(error) {}, nil
	}
	deps.GetFilmDetail = func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
		// 模拟整页全部被业务过滤剔除，返回空列表
		return []model.MovieDetail{}, nil
	}
	deps.SavePage = func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
		return scheduler.Mids{}, nil
	}

	source := &model.FilmSource{Id: "test_empty_pages", Name: "空数据源"}
	hasWrites, succ, fail, failedPages, err := CollectPages(context.Background(), 2, nil, source, 24, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasWrites {
		t.Fatal("expected hasWrites=true for completed empty pages")
	}
	if succ != 2 || fail != 0 {
		t.Fatalf("expected succ=2, fail=0, got succ=%d, fail=%d", succ, fail)
	}
	if len(failedPages) != 0 {
		t.Fatalf("expected no failed pages, got: %v", failedPages)
	}
}
