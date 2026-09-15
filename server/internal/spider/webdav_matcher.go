package spider

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	filmrepo "server/internal/repository/film"
	"server/internal/repository/support"
)

// chineseNum 将数字转换为中文数字（主要支持季数 1-99）
func chineseNum(n int) string {
	digits := []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九", "十"}
	if n >= 0 && n <= 10 {
		return digits[n]
	}
	if n < 20 {
		return "十" + digits[n%10]
	}
	if n < 100 {
		ten := digits[n/10] + "十"
		if n%10 != 0 {
			ten += digits[n%10]
		}
		return ten
	}
	return strconv.Itoa(n)
}

// buildCandidateNames 扩充候选匹配名：
// 若为电视剧且 season > 0，自动追加候选名探测列表以优先匹配 MacCMS 独立分季条目
func buildCandidateNames(lookupName string, isTV bool, season int) []string {
	lookupName = strings.TrimSpace(lookupName)
	if lookupName == "" {
		return nil
	}
	names := []string{lookupName}
	if isTV && season > 0 {
		cn := chineseNum(season)
		names = append(names,
			fmt.Sprintf("%s 第%s季", lookupName, cn),
			fmt.Sprintf("%s第%s季", lookupName, cn),
			fmt.Sprintf("%s 第%d季", lookupName, season),
			fmt.Sprintf("%s第%d季", lookupName, season),
			fmt.Sprintf("%s %d", lookupName, season),
			fmt.Sprintf("%s%d", lookupName, season),
		)
	}
	return uniqueStrings(names)
}

func uniqueStrings(list []string) []string {
	seen := make(map[string]struct{}, len(list))
	out := make([]string, 0, len(list))
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// findRootCategoryPid 查找本地根分类ID (Pid=0)
func findRootCategoryPid(bucket string) int64 {
	if db.Mdb == nil {
		return 0
	}
	var candidates []string
	switch bucket {
	case "movie":
		candidates = []string{"电影"}
	case "tv":
		candidates = []string{"电视剧", "连续剧", "剧集"}
	case "anime":
		candidates = []string{"动漫", "动画", "番剧"}
	case "doc":
		candidates = []string{"纪录片", "纪录", "电影"}
	default:
		candidates = []string{"电影"}
	}

	for _, name := range candidates {
		var cat model.Category
		if err := db.Mdb.Where("pid = 0 AND name = ?", name).First(&cat).Error; err == nil && cat.Id > 0 {
			return cat.Id
		}
	}
	for _, name := range candidates {
		var cat model.Category
		if err := db.Mdb.Where("pid = 0 AND alias LIKE ?", "%"+name+"%").First(&cat).Error; err == nil && cat.Id > 0 {
			return cat.Id
		}
	}
	return 0
}

func uniqueInt64s(list []int64) []int64 {
	seen := make(map[int64]struct{}, len(list))
	out := make([]int64, 0, len(list))
	for _, v := range list {
		if v <= 0 {
			continue
		}
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// MatchMasterSite 执行附属站与 MacCMS 主站的匹配流程：
// 1. 根据 lookupName 展开别名/季候选列表；
// 2. 查 MovieMatchKey 获取候选 mid；
// 3. 对候选 mid 执行 pid 隔离保护（detailPid != infoPid skip）；
// 4. 若全落空，降级到 mappedPid = 0 重新探测候选名；
// 5. 若仍未匹配，判断是否因大类被拒 (category_mismatch) 或纯未匹配 (unmatched)；
// 6. 命中多个 mid 调用 PickBestMidForMatchKey 裁决。
func MatchMasterSite(lookupName string, isTV bool, season int, mappedPid int64) (*model.FilmIndex, string, error) {
	if db.Mdb == nil {
		return nil, "unmatched", errors.New("数据库未连接")
	}
	if mappedPid <= 0 {
		return nil, "no_root_category", errors.New("本地没有电视剧/电影根类，请先同步 MacCMS 主站分类")
	}

	cands := buildCandidateNames(lookupName, isTV, season)
	if len(cands) == 0 {
		return nil, "unmatched", errors.New("候选片名为空")
	}

	hadCategoryMismatch := false

	// 第一轮：使用 mappedPid 严格隔离大类匹配
	for _, candName := range cands {
		keys := filmrepo.BuildMovieMatchKeysWithCategory(0, candName, mappedPid)
		midsMap := filmrepo.LoadMidCandidatesByMatchKeys(keys)
		if len(midsMap) == 0 {
			continue
		}

		var candidateMids []int64
		for _, key := range keys {
			if mids := midsMap[key]; len(mids) > 0 {
				candidateMids = append(candidateMids, mids...)
			}
		}
		candidateMids = uniqueInt64s(candidateMids)
		if len(candidateMids) == 0 {
			continue
		}

		var films []model.FilmIndex
		if err := db.Mdb.Where("mid IN ?", candidateMids).Find(&films).Error; err != nil || len(films) == 0 {
			continue
		}

		var validMids []int64
		filmsByMid := make(map[int64]model.FilmIndex, len(films))
		for _, f := range films {
			filmsByMid[f.Mid] = f
			infoPid := support.GetRootId(f.Pid)
			if infoPid <= 0 && f.Cid > 0 {
				infoPid = support.GetRootId(f.Cid)
			}
			if mappedPid > 0 && infoPid > 0 && infoPid != mappedPid {
				hadCategoryMismatch = true
				continue
			}
			validMids = append(validMids, f.Mid)
		}

		validMids = uniqueInt64s(validMids)
		if len(validMids) > 0 {
			bestMid := filmrepo.PickBestMidForMatchKey(validMids)
			if bestMid > 0 {
				if film, ok := filmsByMid[bestMid]; ok {
					return &film, "scraped", nil
				}
			}
		}
	}

	// 第二轮：降级至 mappedPid = 0（兼容主站未分类或日番电视剧/动漫交叉场景）
	for _, candName := range cands {
		keys := filmrepo.BuildMovieMatchKeysWithCategory(0, candName, 0)
		midsMap := filmrepo.LoadMidCandidatesByMatchKeys(keys)
		if len(midsMap) == 0 {
			continue
		}

		var candidateMids []int64
		for _, key := range keys {
			if mids := midsMap[key]; len(mids) > 0 {
				candidateMids = append(candidateMids, mids...)
			}
		}
		candidateMids = uniqueInt64s(candidateMids)
		if len(candidateMids) == 0 {
			continue
		}

		var films []model.FilmIndex
		if err := db.Mdb.Where("mid IN ?", candidateMids).Find(&films).Error; err != nil || len(films) == 0 {
			continue
		}

		filmsByMid := make(map[int64]model.FilmIndex, len(films))
		for _, f := range films {
			filmsByMid[f.Mid] = f
		}

		bestMid := filmrepo.PickBestMidForMatchKey(candidateMids)
		if bestMid > 0 {
			if film, ok := filmsByMid[bestMid]; ok {
				return &film, "scraped", nil
			}
		}
	}

	if hadCategoryMismatch {
		return nil, "category_mismatch", errors.New("分类不匹配（本地主站分类与媒体类型不一致）")
	}

	return nil, "unmatched", errors.New("未匹配到主站已有影片")
}

func persistScanReport(id uint64, sourceID string, startedAt, finishedAt time.Time, status string, found, parsed, tmdbHit, unmatched, skipped, saved, deleted, failed int, truncated bool, errSummary string) uint64 {
	if db.Mdb == nil {
		return id
	}
	fields := map[string]any{
		"status":        status,
		"found":         found,
		"parsed":        parsed,
		"tmdb_hit":      tmdbHit,
		"unmatched":     unmatched,
		"skipped":       skipped,
		"saved":         saved,
		"deleted":       deleted,
		"failed":        failed,
		"truncated":     truncated,
		"error_summary": errSummary,
	}
	if !finishedAt.IsZero() {
		fields["finished_at"] = finishedAt
	}
	if id > 0 {
		db.Mdb.Model(&model.WebdavScanReport{}).Where("id = ?", id).Updates(fields)
		return id
	}
	report := model.WebdavScanReport{
		SourceId:     sourceID,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		Status:       status,
		Found:        found,
		Parsed:       parsed,
		TmdbHit:      tmdbHit,
		Unmatched:    unmatched,
		Skipped:      skipped,
		Saved:        saved,
		Deleted:      deleted,
		Failed:       failed,
		Truncated:    truncated,
		ErrorSummary: errSummary,
	}
	db.Mdb.Create(&report)

	var oldReportIDs []uint64
	db.Mdb.Model(&model.WebdavScanReport{}).
		Where("source_id = ?", sourceID).
		Order("id DESC").
		Offset(5).
		Pluck("id", &oldReportIDs)
	if len(oldReportIDs) > 0 {
		db.Mdb.Where("id IN ?", oldReportIDs).Delete(&model.WebdavScanReport{})
	}
	return report.ID
}
