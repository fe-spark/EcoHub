package repository

import (
	"sort"
	"strings"

	"server/internal/repository/support"
)

// groupPublicTypes 按规则合成前台分类树。
// 一级改名后原名落到目标下作为二级；二级只改名合并，不再往下挂一层。
// 隐藏的 type 仍参与合并，组是否上前台看代表分类的 show。
func groupPublicTypes(rows []sourceTypeRow, sourceID string) []publicTypeGroup {
	childrenOf := map[int64][]sourceTypeRow{}
	roots := make([]sourceTypeRow, 0)
	for _, row := range rows {
		if row.Parent > 0 {
			childrenOf[row.Parent] = append(childrenOf[row.Parent], row)
			continue
		}
		roots = append(roots, row)
	}

	type bucket struct {
		members []sourceTypeRow
		renamed []sourceTypeRow
	}
	order := make([]string, 0)
	buckets := map[string]*bucket{}
	for _, row := range roots {
		name := row.Name
		if mapped := strings.TrimSpace(support.NormalizeRootCategoryName(sourceID, row.Name)); mapped != "" {
			name = mapped
		}
		item, ok := buckets[name]
		if !ok {
			item = &bucket{}
			buckets[name] = item
			order = append(order, name)
		}
		item.members = append(item.members, row)
		if name != row.Name {
			item.renamed = append(item.renamed, row)
		}
	}

	groups := make([]publicTypeGroup, 0, len(order))
	for _, name := range order {
		item := buckets[name]
		group := makePublicGroup(name, item.members)
		childRows := append([]sourceTypeRow{}, item.renamed...)
		for _, row := range item.members {
			for _, child := range childrenOf[row.ID] {
				child.Name = support.NormalizeSubCategoryName(sourceID, child.Name)
				childRows = append(childRows, child)
			}
		}
		group.Children = groupTypeRows(childRows)
		for j := range group.Children {
			group.Children[j].Parent = group.ID
		}
		groups = append(groups, group)
	}
	sortPublicGroups(groups)
	return groups
}

func groupTypeRows(rows []sourceTypeRow) []publicTypeGroup {
	order := make([]string, 0)
	buckets := map[string][]sourceTypeRow{}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			name = row.Name
		}
		if _, ok := buckets[name]; !ok {
			order = append(order, name)
		}
		buckets[name] = append(buckets[name], row)
	}
	groups := make([]publicTypeGroup, 0, len(order))
	for _, name := range order {
		groups = append(groups, makePublicGroup(name, buckets[name]))
	}
	sortPublicGroups(groups)
	return groups
}

func makePublicGroup(name string, members []sourceTypeRow) publicTypeGroup {
	group := publicTypeGroup{
		ID: members[0].ID, Name: name, Sort: members[0].Sort, Show: members[0].Show,
		Members: make([]int64, 0, len(members)),
	}
	for _, row := range members {
		group.Members = append(group.Members, row.ID)
		if row.ID < group.ID {
			group.ID = row.ID
			group.Sort = row.Sort
			group.Show = row.Show
		}
	}
	sort.Slice(group.Members, func(i, j int) bool { return group.Members[i] < group.Members[j] })
	return group
}

func sortPublicGroups(groups []publicTypeGroup) {
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Sort != groups[j].Sort {
			return groups[i].Sort < groups[j].Sort
		}
		return groups[i].ID < groups[j].ID
	})
}

func indexPublicGroups(groups []publicTypeGroup, byID, roots map[int64]publicTypeGroup) {
	for i := range groups {
		group := groups[i]
		for _, member := range group.Members {
			byID[member] = group
			roots[member] = group
		}
		byID[group.ID] = group
		roots[group.ID] = group
		for j := range group.Children {
			child := group.Children[j]
			for _, member := range child.Members {
				byID[member] = child
			}
			byID[child.ID] = child
		}
	}
}
