package repository

import (
	"sort"
	"strings"

	"server/internal/repository/support"
)

// groupPublicTypes 按规则合成前台分类树。
// 一级改名后原名落到目标下作为二级；二级只改名合并，不再往下挂一层。
// 隐藏的 type 仍参与合并，只要组内存在未隐藏成员则合并组保持显示。
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
		native  []sourceTypeRow
		renamed []sourceTypeRow
		all     []sourceTypeRow
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
		item.all = append(item.all, row)
		if name != row.Name {
			item.renamed = append(item.renamed, row)
		} else {
			item.native = append(item.native, row)
		}
	}

	groups := make([]publicTypeGroup, 0, len(order))
	for _, name := range order {
		item := buckets[name]
		var group publicTypeGroup
		if len(item.native) > 0 {
			group = makePublicGroup(name, item.native)
		} else {
			// 所有命中规则的一级分类均被改名到新组名：父组使用独立合成行，不再借用首个子项的数据行。
			seed := makePublicGroup(name, item.all)
			group = publicTypeGroup{
				Name:      name,
				Sort:      seed.Sort,
				Show:      true,
				Synthetic: true,
			}
		}
		for _, row := range item.renamed {
			if row.ID > 0 {
				group.RootMemberIDs = append(group.RootMemberIDs, row.ID)
			}
		}
		childRows := make([]sourceTypeRow, 0, len(item.renamed))
		for _, row := range item.renamed {
			row.Name = support.NormalizeSubCategoryName(sourceID, row.Name)
			childRows = append(childRows, row)
		}
		for _, row := range item.all {
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
	rep := members[0]
	group := publicTypeGroup{
		Name:    name,
		Members: make([]int64, 0, len(members)),
	}
	for _, row := range members {
		group.Members = append(group.Members, row.ID)
		if preferGroupRepresentative(row, rep) {
			rep = row
		}
	}
	group.ID = rep.ID
	group.Sort = rep.Sort
	group.Show = rep.Show
	sort.Slice(group.Members, func(i, j int) bool { return group.Members[i] < group.Members[j] })
	return group
}

func preferGroupRepresentative(candidate, current sourceTypeRow) bool {
	if candidate.Show != current.Show {
		return candidate.Show
	}
	return candidate.ID < current.ID
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
		if group.ID > 0 {
			byID[group.ID] = group
			roots[group.ID] = group
		}
		for _, member := range group.Members {
			byID[member] = group
			roots[member] = group
		}
		for _, member := range group.RootMemberIDs {
			if member > 0 {
				roots[member] = group
			}
		}
		for j := range group.Children {
			child := group.Children[j]
			child.Parent = group.ID
			for _, member := range child.Members {
				byID[member] = child
			}
			byID[child.ID] = child
		}
	}
}
