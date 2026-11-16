package snapshot

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
)

// currentPrimarySourceID 当前主站。没有已启用采集站时为空。
func currentPrimarySourceID() string {
	primary := repository.PickPrimarySourceForCategory()
	if primary == nil {
		return ""
	}
	return strings.TrimSpace(primary.Id)
}

func indexMatches(cur *filmSearchMetaIndex, version, primary string) bool {
	return cur != nil && cur.Version != "" && cur.Version == version && cur.PrimaryID == primary
}

// RebuildPrimarySearchIndex 丢掉当前索引，只装当前主站。
func RebuildPrimarySearchIndex() error {
	version := currentSearchIndexVersion()
	if strings.TrimSpace(version) == "" || db.Mdb == nil {
		return ErrSearchIndexMissing
	}
	activeFilmSearchMetasMu.Lock()
	activeFilmSearchMetas.Store(nil)
	activeFilmSearchMetasMu.Unlock()
	_, err := buildAndStoreFilmSearchMetaIndex(version)
	return err
}

// PrimarySearchMIDs 只留下查到且属于当前主站的影片编号。
// 没有主站、或库里没有这行、或所属站不是主站时都不留下。
// 数据库未连接时无法判断所属站，原样返回，避免测试里的封口队列被清空。
// 查询失败返回 error。调用方应按原编号点查，不得当成没有主站影片，也不得因此全表重建。
func PrimarySearchMIDs(mids []int64) ([]int64, error) {
	if len(mids) == 0 {
		return nil, nil
	}
	if db.Mdb == nil {
		return append([]int64(nil), mids...), nil
	}
	primary := currentPrimarySourceID()
	if primary == "" {
		return nil, nil
	}
	const batchSize = 2500
	owned := make(map[int64]string, len(mids))
	for i := 0; i < len(mids); i += batchSize {
		end := i + batchSize
		if end > len(mids) {
			end = len(mids)
		}
		var rows []struct {
			Mid           int64
			FirstSourceId string
		}
		if err := db.Mdb.Model(&model.FilmIndex{}).
			Select("mid, first_source_id").
			Where("mid IN ?", mids[i:end]).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			owned[row.Mid] = strings.TrimSpace(row.FirstSourceId)
		}
	}
	kept := make([]int64, 0, len(mids))
	for _, mid := range mids {
		src, ok := owned[mid]
		if !ok || src != primary {
			continue
		}
		kept = append(kept, mid)
	}
	return kept, nil
}
