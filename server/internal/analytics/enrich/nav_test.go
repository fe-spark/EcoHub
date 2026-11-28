package enrich

import (
	"testing"

	"server/internal/analytics/types"
	"server/internal/model"
)

func sampleActiveCategoryTree() model.CategoryTree {
	return model.CategoryTree{
		Id:   0,
		Pid:  -1,
		Name: "root",
		Show: true,
		Children: []*model.CategoryTree{
			{
				Id:        10,
				Pid:       0,
				Name:      "短剧",
				Show:      true,
				MemberIds: []int64{10, 910},
				Children: []*model.CategoryTree{
					{
						Id:        101,
						Pid:       10,
						Name:      "现代都市",
						Show:      true,
						MemberIds: []int64{101, 9101},
					},
					{
						Id:   102,
						Pid:  10,
						Name: "隐藏子类",
						Show: false,
					},
				},
			},
			{
				Id:   20,
				Pid:  0,
				Name: "综艺",
				Show: true,
			},
			{
				Id:   30,
				Pid:  0,
				Name: "隐藏大类",
				Show: false,
				Children: []*model.CategoryTree{
					{
						Id:   301,
						Pid:  30,
						Name: "隐藏大类下的子类",
						Show: true,
					},
				},
			},
		},
	}
}

func TestBuildCategoryNavIndex(t *testing.T) {
	idx := BuildCategoryNavIndex(sampleActiveCategoryTree())

	// 一级可见大类 -> /category?pid=10
	got, ok := idx.LookupByID(10)
	if !ok || got.Href != "/category?pid=10" || got.Name != "短剧" {
		t.Fatalf("一级大类映射异常: ok=%v got=%+v", ok, got)
	}

	// 一级大类合并成员 ID -> 归一化到展示节点
	got, ok = idx.LookupByID(910)
	if !ok || got.ID != 10 || got.Href != "/category?pid=10" {
		t.Fatalf("一级合并成员映射异常: ok=%v got=%+v", ok, got)
	}

	// 二级可见子分类 -> /category/filter?pid=10&category=101
	got, ok = idx.LookupByID(101)
	if !ok || got.Href != "/category/filter?pid=10&category=101" || got.Name != "现代都市" {
		t.Fatalf("二级子分类映射异常: ok=%v got=%+v", ok, got)
	}

	// 二级子分类合并成员 ID
	got, ok = idx.LookupByID(9101)
	if !ok || got.ID != 101 || got.Href != "/category/filter?pid=10&category=101" {
		t.Fatalf("二级合并成员映射异常: ok=%v got=%+v", ok, got)
	}

	// 按名称命中二级子分类
	got, ok = idx.LookupByName("现代都市")
	if !ok || got.Href != "/category/filter?pid=10&category=101" {
		t.Fatalf("按名称查找二级子分类异常: ok=%v got=%+v", ok, got)
	}

	// 已隐藏子类或隐藏大类及其下子类不应生成跳转链接
	if _, ok := idx.LookupByID(102); ok {
		t.Fatalf("隐藏子类不应命中导航索引")
	}
	if _, ok := idx.LookupByID(30); ok {
		t.Fatalf("隐藏大类不应命中导航索引")
	}
	if _, ok := idx.LookupByID(301); ok {
		t.Fatalf("隐藏大类下的子类不应命中导航索引")
	}
}

func TestTakeClassifyTopsAndEnrichLogHref(t *testing.T) {
	origLoader := loadActiveCategoryTree
	loadActiveCategoryTree = func() (model.CategoryTree, error) {
		return sampleActiveCategoryTree(), nil
	}
	t.Cleanup(func() {
		loadActiveCategoryTree = origLoader
	})

	tops, err := TakeClassifyTops([]types.TopItem{
		{Key: "101", Count: 3},
		{Key: "现代都市", Count: 2},
		{Key: "综艺", Count: 4},
		{Key: "已停用旧分类", Count: 1},
	}, 10)
	if err != nil {
		t.Fatalf("TakeClassifyTops 失败: %v", err)
	}

	byTitle := make(map[string]types.TopItem, len(tops))
	for _, it := range tops {
		byTitle[it.Title] = it
	}

	// 101 与 "现代都市" 应合并为一条并带筛选页链接
	if it := byTitle["现代都市"]; it.Count != 5 || it.Href != "/category/filter?pid=10&category=101" {
		t.Fatalf("二级子分类汇总或链接异常: %+v", it)
	}
	// "综艺" 是一级大类
	if it := byTitle["综艺"]; it.Count != 4 || it.Href != "/category?pid=20" {
		t.Fatalf("一级大类链接异常: %+v", it)
	}
	// 不存在于主站可见树中的分类不应生成死链
	if it := byTitle["已停用旧分类"]; it.Count != 1 || it.Href != "" {
		t.Fatalf("无效分类不应生成跳转链接: %+v", it)
	}

	events, err := EnrichLogEvents([]types.Event{
		{Action: types.ActionClassify, Resource: "101"},
		{Action: types.ActionClassify, Resource: "20"},
		{Action: types.ActionClassify, Resource: "99999", ResourceCat: "已下架分类"},
	})
	if err != nil {
		t.Fatalf("EnrichLogEvents 失败: %v", err)
	}
	if events[0].ResourceHref != "/category/filter?pid=10&category=101" || events[0].ResourceCat != "现代都市" {
		t.Fatalf("流水二级分类补全异常: %+v", events[0])
	}
	if events[1].ResourceHref != "/category?pid=20" || events[1].ResourceCat != "综艺" {
		t.Fatalf("流水一级分类补全异常: %+v", events[1])
	}
	if events[2].ResourceHref != "" {
		t.Fatalf("无效流水分类不应生成链接: %+v", events[2])
	}
}
