package snapshot

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

const LiveReadVersion = "live"

func liveFilmQuery() *gorm.DB {
	return db.Mdb.Model(&model.FilmIndex{})
}

func applySourceMembership(query *gorm.DB, sourceID string) *gorm.DB {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || query == nil {
		return query
	}
	return query.Where(model.FilmHasPlaySourceSQL(), sourceID, "play")
}

func resolveListVersion(version string) string {
	version = strings.TrimSpace(version)
	if version != "" {
		return version
	}
	if v := strings.TrimSpace(GetActiveSnapshotVersion()); v != "" {
		return v
	}
	return LiveReadVersion
}

func EnsureLiveReadVersion() string {
	version := strings.TrimSpace(GetActiveSnapshotVersion())
	if version != "" {
		return version
	}
	_ = SetActiveSnapshotVersion(LiveReadVersion)
	return LiveReadVersion
}

func indexesToListSnapshots(indexes []model.FilmIndex) []model.FilmListSnapshot {
	if len(indexes) == 0 {
		return []model.FilmListSnapshot{}
	}
	out := make([]model.FilmListSnapshot, 0, len(indexes))
	for _, index := range indexes {
		if index.Mid <= 0 {
			continue
		}
		out = append(out, buildFilmListSnapshot("", index))
	}
	return out
}

func scanListSnapshots(query *gorm.DB) ([]model.FilmListSnapshot, error) {
	var indexes []model.FilmIndex
	if err := query.Find(&indexes).Error; err != nil {
		return nil, err
	}
	return indexesToListSnapshots(indexes), nil
}

func listSnapshotToIndex(s model.FilmListSnapshot) model.FilmIndex {
	return model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: s.Mid, FirstSourceId: s.SourceId, DbId: s.DbId},
		FilmIndexCategory: model.FilmIndexCategory{
			Cid: s.Cid, Pid: s.Pid, RootCategoryKey: s.RootCategoryKey, CategoryKey: s.CategoryKey,
			OriginalCategory: s.OriginalCategory, CName: s.CName,
		},
		FilmIndexContent: model.FilmIndexContent{
			SeriesKey: s.SeriesKey, Name: s.Name, SubTitle: s.SubTitle, ClassTag: s.ClassTag,
			Area: s.Area, Language: s.Language, Year: s.Year, Initial: s.Initial, Score: s.Score,
			UpdateStamp: s.UpdateStamp, UpdateReason: s.UpdateReason, Hits: s.Hits, State: s.State,
			Remarks: s.Remarks, Picture: s.Picture, PictureSlide: s.PictureSlide,
			CustomPicture: s.CustomPicture, CustomPictureSlide: s.CustomPictureSlide, IsCustomPicture: s.IsCustomPicture,
			Actor: s.Actor, Director: s.Director, Writer: s.Writer, Blurb: s.Blurb, Content: s.Content, ReleaseDate: s.ReleaseDate,
		},
		FilmIndexVersion: model.FilmIndexVersion{CollectStamp: s.CollectStamp, CategoryVersion: s.CategoryVersion, RuleVersion: s.RuleVersion},
		FilmIndexDerived: model.FilmIndexDerived{PlayFromSummary: s.PlayFromSummary},
	}
}

// WriteLiveFilmsFromSnapshots 测试/迁移辅助：把列表快照行落到 film_index（及可选线路成员）。
func WriteLiveFilmsFromSnapshots(snaps []model.FilmListSnapshot) error {
	if db.Mdb == nil || len(snaps) == 0 {
		return nil
	}
	for _, s := range snaps {
		if s.Mid <= 0 {
			continue
		}
		idx := listSnapshotToIndex(s)
		if err := db.Mdb.Create(&idx).Error; err != nil {
			return err
		}
	}
	return nil
}

func liveTieOrder(order string) string {
	order = strings.TrimSpace(order)
	if order == "" {
		return "update_stamp DESC, mid DESC"
	}
	order = strings.ReplaceAll(order, ", id DESC", ", mid DESC")
	order = strings.ReplaceAll(order, " id DESC", " mid DESC")
	return order
}

func findListPage(query *gorm.DB, order string, offset, limit int) ([]model.FilmListSnapshot, error) {
	order = liveTieOrder(order)
	var mids []int64
	q := query.Select("mid").Order(order)
	if offset > 0 {
		q = q.Offset(offset)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Pluck("mid", &mids).Error; err != nil {
		return nil, err
	}
	if len(mids) == 0 {
		return []model.FilmListSnapshot{}, nil
	}
	return GetSnapshotsByMidsOrdered("", mids), nil
}
