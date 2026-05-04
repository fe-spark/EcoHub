package film

import (
	"log"
	"strconv"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

type filterIndexClause struct {
	TagType  string
	TagValue string
}

func RebuildFilterIndexSnapshot(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}

	startedAt := time.Now()
	var total int
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("snapshot_version = ?", version).Unscoped().Delete(&model.FilmFilterIndexSnapshot{}).Error; err != nil {
			return err
		}

		lastID := uint(0)
		for {
			var snapshots []model.FilmListSnapshot
			if err := tx.Where("snapshot_version = ? AND id > ?", version, lastID).
				Order("id ASC").Limit(snapshotBuildBatchSize).Find(&snapshots).Error; err != nil {
				return err
			}
			if len(snapshots) == 0 {
				return nil
			}

			indexes := make([]model.FilmFilterIndexSnapshot, 0, len(snapshots)*6)
			for _, snapshot := range snapshots {
				indexes = append(indexes, buildFilterIndexRows(version, snapshot)...)
				lastID = snapshot.ID
			}
			if len(indexes) > 0 {
				if err := tx.CreateInBatches(indexes, 1000).Error; err != nil {
					return err
				}
				total += len(indexes)
			}
		}
	})
	if err != nil {
		return err
	}
	log.Printf("[FilterIndexSnapshot] 重建完成 version=%s rows=%d cost=%s", version, total, time.Since(startedAt))
	return nil
}

func buildFilterIndexRows(version string, snapshot model.FilmListSnapshot) []model.FilmFilterIndexSnapshot {
	rows := make([]model.FilmFilterIndexSnapshot, 0, 6)
	appendRow := func(tagType string, tagValue string) {
		tagValue = strings.TrimSpace(tagValue)
		if tagValue == "" {
			return
		}
		rows = append(rows, model.FilmFilterIndexSnapshot{
			SnapshotVersion: version,
			Pid:             support.ResolveCategoryID(snapshot.Pid),
			Cid:             support.ResolveCategoryID(snapshot.Cid),
			TagType:         tagType,
			TagValue:        tagValue,
			Mid:             snapshot.Mid,
			Year:            snapshot.Year,
			UpdateStamp:     snapshot.UpdateStamp,
			Hits:            snapshot.Hits,
			Score:           snapshot.Score,
		})
	}

	if snapshot.Cid > 0 {
		appendRow("Category", strconv.FormatInt(support.ResolveCategoryID(snapshot.Cid), 10))
	} else if snapshot.Pid > 0 {
		appendRow("Category", strconv.FormatInt(support.ResolveCategoryID(snapshot.Pid), 10))
	}
	for _, tag := range splitClassTags(snapshot.ClassTag) {
		appendRow("Plot", tag)
	}
	appendRow("Area", snapshot.Area)
	appendRow("Language", snapshot.Language)
	if snapshot.Year > 0 {
		appendRow("Year", strconv.FormatInt(snapshot.Year, 10))
	}
	return rows
}

func EnsureActiveFilterIndexSnapshot() error {
	version := GetActiveSnapshotVersion()
	if version == "" {
		return nil
	}

	var count int64
	if err := db.Mdb.Model(&model.FilmFilterIndexSnapshot{}).Where("snapshot_version = ?", version).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return RebuildFilterIndexSnapshot(version)
}

func buildFilterIndexClauses(st *model.SearchTagsVO) ([]filterIndexClause, bool) {
	clauses := make([]filterIndexClause, 0, 5)
	if st.Cid > 0 {
		if st.Cid == model.TagUncategorizedValue {
			return nil, false
		}
		if support.IsRootCategory(st.Cid) {
			st.Pid = support.ResolveCategoryID(st.Cid)
			st.Cid = 0
		} else {
			clauses = append(clauses, filterIndexClause{TagType: "Category", TagValue: strconv.FormatInt(support.ResolveCategoryID(st.Cid), 10)})
		}
	}
	if !appendNormalFilterIndexClause(&clauses, "Plot", st.Plot) {
		return nil, false
	}
	if !appendNormalFilterIndexClause(&clauses, "Area", st.Area) {
		return nil, false
	}
	if !appendNormalFilterIndexClause(&clauses, "Language", st.Language) {
		return nil, false
	}
	if !appendNormalFilterIndexClause(&clauses, "Year", st.Year) {
		return nil, false
	}
	return clauses, len(clauses) > 0
}

func appendNormalFilterIndexClause(clauses *[]filterIndexClause, tagType string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if value == model.TagUnknownValue || value == model.TagOthersValue {
		return false
	}
	*clauses = append(*clauses, filterIndexClause{TagType: tagType, TagValue: value})
	return true
}

func pageSnapshots(snapshots []model.FilmListSnapshot, page *dto.Page) []model.FilmListSnapshot {
	offset := getPageOffset(page)
	if offset >= len(snapshots) {
		return []model.FilmListSnapshot{}
	}
	end := offset + page.PageSize
	if end > len(snapshots) {
		end = len(snapshots)
	}
	return snapshots[offset:end]
}
