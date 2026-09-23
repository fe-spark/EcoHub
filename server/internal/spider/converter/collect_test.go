package converter

import (
	"testing"

	"server/internal/model"
)

func TestInferCategoryParentsBySemantic(t *testing.T) {
	classes := []model.FilmClass{
		{ID: 1, Name: "电影"},
		{ID: 2, Name: "电视剧"},
		{ID: 3, Name: "动漫"},
		{ID: 4, Name: "综艺"},
		{ID: 5, Name: "纪录片"},
		{ID: 6, Name: "动作片"},
		{ID: 7, Name: "爱情片"},
		{ID: 13, Name: "动漫电影"},
		{ID: 14, Name: "大陆剧"},
		{ID: 16, Name: "韩剧"},
		{ID: 17, Name: "美剧"},
		{ID: 23, Name: "体育赛事"},
		{ID: 24, Name: "中国动漫"},
		{ID: 25, Name: "日本动漫"},
		{ID: 27, Name: "短剧"},
		{ID: 29, Name: "足球"},
		{ID: 33, Name: "大陆综艺"},
		{ID: 37, Name: "古装仙侠"},
		{ID: 38, Name: "现代都市"},
		{ID: 42, Name: "反转爽剧"},
		{ID: 45, Name: "AI漫剧"},
		{ID: 46, Name: "悬疑片"},
		{ID: 47, Name: "都市剧"},
	}

	hints := InferCategoryParentsBySemantic(classes)
	if hints == nil {
		t.Fatal("expected non-nil hints")
	}

	// 电影子类
	if hints[6] != 1 {
		t.Errorf("expected 动作片 -> 电影 (1), got %d", hints[6])
	}
	if hints[7] != 1 {
		t.Errorf("expected 爱情片 -> 电影 (1), got %d", hints[7])
	}
	if hints[13] != 1 {
		t.Errorf("expected 动漫电影 -> 电影 (1), got %d", hints[13])
	}
	if hints[46] != 1 {
		t.Errorf("expected 悬疑片 -> 电影 (1), got %d", hints[46])
	}

	// 电视剧子类
	if hints[14] != 2 {
		t.Errorf("expected 大陆剧 -> 电视剧 (2), got %d", hints[14])
	}
	if hints[16] != 2 {
		t.Errorf("expected 韩剧 -> 电视剧 (2), got %d", hints[16])
	}
	if hints[17] != 2 {
		t.Errorf("expected 美剧 -> 电视剧 (2), got %d", hints[17])
	}
	if hints[47] != 2 {
		t.Errorf("expected 都市剧 -> 电视剧 (2), got %d", hints[47])
	}

	// 动漫子类
	if hints[24] != 3 {
		t.Errorf("expected 中国动漫 -> 动漫 (3), got %d", hints[24])
	}
	if hints[25] != 3 {
		t.Errorf("expected 日本动漫 -> 动漫 (3), got %d", hints[25])
	}

	// 综艺子类
	if hints[33] != 4 {
		t.Errorf("expected 大陆综艺 -> 综艺 (4), got %d", hints[33])
	}

	// 体育子类
	if hints[29] != 23 {
		t.Errorf("expected 足球 -> 体育赛事 (23), got %d", hints[29])
	}

	// 短剧子类
	if hints[37] != 27 {
		t.Errorf("expected 古装仙侠 -> 短剧 (27), got %d", hints[37])
	}
	if hints[38] != 27 {
		t.Errorf("expected 现代都市 -> 短剧 (27), got %d", hints[38])
	}
	if hints[42] != 27 {
		t.Errorf("expected 反转爽剧 -> 短剧 (27), got %d", hints[42])
	}

	// 主类自身不应出现在 hints 中
	if _, ok := hints[1]; ok {
		t.Errorf("root 电影 should not be in hints")
	}
	if _, ok := hints[2]; ok {
		t.Errorf("root 电视剧 should not be in hints")
	}
	if _, ok := hints[27]; ok {
		t.Errorf("root 短剧 should not be in hints")
	}
}

func TestGenCategoryTreeWithParentHints_UnifiedFallback(t *testing.T) {
	// 采集源返回无 pid 的列表，调用方传入 parentHints=nil
	classes := []model.FilmClass{
		{ID: 1, Name: "电影"},
		{ID: 2, Name: "电视剧"},
		{ID: 6, Name: "动作片"},
		{ID: 14, Name: "大陆剧"},
	}

	tree := GenCategoryTreeWithParentHints(classes, nil)
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}

	// 应该有 2 个主类：电影、电视剧
	if len(tree.Children) != 2 {
		t.Fatalf("expected 2 root categories, got %d", len(tree.Children))
	}

	var movieNode, tvNode *model.CategoryTree
	for _, node := range tree.Children {
		if node.Id == 1 {
			movieNode = node
		} else if node.Id == 2 {
			tvNode = node
		}
	}

	if movieNode == nil {
		t.Fatal("expected movie root node")
	}
	if len(movieNode.Children) != 1 || movieNode.Children[0].Id != 6 {
		t.Fatalf("expected 动作片 under 电影, got children count: %d", len(movieNode.Children))
	}

	if tvNode == nil {
		t.Fatal("expected tv root node")
	}
	if len(tvNode.Children) != 1 || tvNode.Children[0].Id != 14 {
		t.Fatalf("expected 大陆剧 under 电视剧, got children count: %d", len(tvNode.Children))
	}
}
