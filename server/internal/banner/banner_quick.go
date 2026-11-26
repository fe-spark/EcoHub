package banner

import (
	"context"
	"strings"
	"time"

	"server/internal/film/catalog"
	"server/internal/film/snapshot"
	"server/internal/hotchart"
	"server/internal/model"
	"server/internal/taxonomy"
)

// 一键排片单个分类最多排几部，和后台输入框上限一致。
const bannerQuotaMax = 5

// 一键排片查询的超时上限。
const bannerGenerateTimeout = 10 * time.Second

// BannerQuota 一键排片里一个分类排几部。
type BannerQuota struct {
	CategoryID int64 `json:"categoryId"`
	Count      int   `json:"count"`
}

// BannerGenerateReq 一键排片请求。策略和数量都由前端提交，服务端不保存。
type BannerGenerateReq struct {
	SourceId string        `json:"sourceId"`
	Strategy string        `json:"strategy"`
	Quotas   []BannerQuota `json:"quotas"`
}

// BannerSkipped 一键排片里被跳过或数量不足的分类。
type BannerSkipped struct {
	CategoryID int64  `json:"categoryId"`
	Reason     string `json:"reason"` // not_site_category | not_enough
	Requested  int    `json:"requested"`
	Got        int    `json:"got"`
}

// BannerGenerateResult 一键排片预览。revision 是生成时该站轮播的版本号，发布时带回。
type BannerGenerateResult struct {
	SourceId        string                  `json:"sourceId"`
	PrimarySourceId string                  `json:"primarySourceId"`
	Revision        string                  `json:"revision"`
	Items           []model.BannerAdminItem `json:"items"`
	Skipped         []BannerSkipped         `json:"skipped"`
}

var bannerStrategies = map[string]string{
	"latest": "latest_random",
	"hot":    "hot_random",
	"score":  "score_random",
	"random": "random",
	"douban": "douban",
}

// SnapshotStrategyKey 返回排片策略对应的快照检索策略标识。
func SnapshotStrategyKey(strategy string) (string, bool) {
	v, ok := bannerStrategies[strings.TrimSpace(strategy)]
	return v, ok
}

// Generate 按策略和每个分类的数量为指定采集站生成预览，不落库、不保存配置。
func (s *BannerService) Generate(ctx context.Context, req BannerGenerateReq) (BannerGenerateResult, error) {
	src, _, primaryID, err := resolveBannerSource(req.SourceId, true)
	if err != nil {
		return BannerGenerateResult{}, err
	}
	strategy, ok := bannerStrategies[strings.TrimSpace(req.Strategy)]
	if !ok {
		return BannerGenerateResult{}, bannerInputErr("排片策略只能是 latest、hot、score、random 或 douban")
	}
	quotas := make([]BannerQuota, 0, len(req.Quotas))
	seen := make(map[int64]bool, len(req.Quotas))
	for _, q := range req.Quotas {
		if q.CategoryID <= 0 {
			return BannerGenerateResult{}, bannerInputErr("分类编号不合法")
		}
		if q.Count < 0 || q.Count > bannerQuotaMax {
			return BannerGenerateResult{}, bannerInputErr("每个分类的数量只能在 0 到 %d 之间", bannerQuotaMax)
		}
		if q.Count == 0 || seen[q.CategoryID] {
			continue
		}
		seen[q.CategoryID] = true
		quotas = append(quotas, q)
	}
	if len(quotas) == 0 {
		return BannerGenerateResult{}, bannerInputErr("请至少为一个分类设置排片数量")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, bannerGenerateTimeout)
	defer cancel()

	current, err := ListBannerSlotsCtx(ctx, src.Id)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return BannerGenerateResult{}, ctxErr
		}
		return BannerGenerateResult{}, err
	}
	version := snapshot.GetActiveSnapshotVersion()
	readVersion := snapshot.GetActiveReadModelVersion()
	var sourceCategoryNames map[int64]string
	if strategy == "douban" {
		if tree, treeErr := catalog.GetActiveCategoryTree(src.Id); treeErr == nil {
			sourceCategoryNames = make(map[int64]string, len(tree.Children))
			for _, ch := range tree.Children {
				if ch != nil && ch.Id > 0 {
					sourceCategoryNames[ch.Id] = ch.Name
				}
			}
		}
	}

	picks := make([][]model.FilmListSnapshot, len(quotas))
	skipped := make([]BannerSkipped, 0)
	used := make([]int64, 0)
	for i, q := range quotas {
		if err := ctx.Err(); err != nil {
			return BannerGenerateResult{}, err
		}
		cats, fellBack := ResolveBannerSourceCategories(src.Id, []int64{q.CategoryID})
		if err := ctx.Err(); err != nil {
			return BannerGenerateResult{}, err
		}
		if fellBack || len(cats) == 0 {
			skipped = append(skipped, BannerSkipped{CategoryID: q.CategoryID, Reason: "not_site_category", Requested: q.Count})
			continue
		}
		var cands []model.FilmListSnapshot
		if strategy == "douban" {
			catName := strings.TrimSpace(sourceCategoryNames[q.CategoryID])
			if catName == "" {
				catName = taxonomy.GetCategoryNameById(q.CategoryID)
			}
			rawCands, err := hotchart.HotChartsSvc.GetRawCandidatesByCategoryName(catName)
			if err != nil {
				return BannerGenerateResult{}, bannerInputErr("获取豆瓣推荐榜单失败: %v", err)
			}
			mids, _, err := snapshot.MatchHotChartCandidates(readVersion, src.Id, rawCands, q.Count, used...)
			if err != nil {
				return BannerGenerateResult{}, err
			}
			cands, err = snapshot.ListSnapshotsByMIDsForHotCharts(ctx, mids)
			if err != nil {
				return BannerGenerateResult{}, err
			}
		} else {
			cands, err = snapshot.GetSnapshotBannerCandidates(ctx, version, strategy, src.Id, cats, q.Count, used)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return BannerGenerateResult{}, ctxErr
				}
				return BannerGenerateResult{}, err
			}
		}
		picks[i] = cands
		for _, c := range cands {
			used = append(used, c.Mid)
		}
		if len(cands) < q.Count {
			skipped = append(skipped, BannerSkipped{CategoryID: q.CategoryID, Reason: "not_enough", Requested: q.Count, Got: len(cands)})
		}
	}

	// 各分类轮流穿插。
	slots := make([]model.BannerSlot, 0, len(used))
	for idx := 0; ; idx++ {
		added := false
		for _, list := range picks {
			if idx < len(list) {
				slots = append(slots, model.BannerSlot{
					SourceId: src.Id, Mid: list[idx].Mid, Sort: int64(len(slots) + 1), ImageMode: model.BannerImageFollow,
				})
				added = true
			}
		}
		if !added {
			break
		}
	}
	items, err := AssembleBanners(ctx, slots, false)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return BannerGenerateResult{}, ctxErr
		}
		return BannerGenerateResult{}, err
	}
	return BannerGenerateResult{
		SourceId:        src.Id,
		PrimarySourceId: primaryID,
		Revision:        BannerRevision(current),
		Items:           items,
		Skipped:         skipped,
	}, nil
}

// ResolveBannerSourceCategories 排片分类只认首选站自己的一级分类。
// 没选表示不限。旧的整库展示分类编号对不上这个站时，退回该站全部一级分类。
func ResolveBannerSourceCategories(sourceID string, selected []int64) (ids []int64, fellBack bool) {
	roots := catalog.SourceRootTypeIDs(sourceID)
	if len(selected) == 0 {
		return roots, false
	}
	allow := make(map[int64]struct{}, len(roots))
	for _, id := range roots {
		allow[id] = struct{}{}
	}
	out := make([]int64, 0, len(selected))
	seen := make(map[int64]struct{}, len(selected))
	for _, id := range selected {
		if _, ok := allow[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return roots, true
	}
	return out, false
}
