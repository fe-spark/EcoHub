package writer

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"reflect"
	"sort"
	"strings"
	"time"

	"server/internal/film/shared"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ownedFilmSyncSelect 增量同步比对所需的最小字段集，避免 SELECT * 额外拉取无关列与自定义封面大字段。
const ownedFilmSyncSelect = "mid, first_source_id, db_id, cid, pid, root_category_key, category_key, original_category, c_name, series_key, name, sub_title, class_tag, area, language, year, initial, score, hits, state, actor, director, writer, blurb, content, release_date, picture, picture_slide, is_custom_picture, locked_fields"

func createNewFilmIndexTx(tx *gorm.DB, sourceID string, detail model.MovieDetail) (*model.FilmIndex, error) {
	categoryVersion := taxonomy.GetCategoryVersion()
	ruleVersion := taxonomy.GetRuleVersion()
	fi, err := ConvertFilmIndex(sourceID, detail, categoryVersion, ruleVersion)
	if err != nil {
		return nil, err
	}
	fi.Mid = 0 // 自增生成
	fi.FirstSourceId = sourceID

	if err := tx.Create(&fi).Error; err != nil {
		return nil, err
	}

	pid := fi.Pid
	if pid <= 0 {
		pid = detail.RawPid
	}
	allKeys := shared.BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
	sort.Strings(allKeys)
	if len(allKeys) > 0 {
		matchKeys := make([]model.MovieMatchKey, 0, len(allKeys))
		for _, k := range allKeys {
			matchKeys = append(matchKeys, model.MovieMatchKey{
				Mid:      fi.Mid,
				MatchKey: k,
			})
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&matchKeys).Error; err != nil {
			return nil, err
		}
	}

	return &fi, nil
}

// syncOwnedFilmTx 用本站最新采集结果更新自己的档案。不改其他站的档案，也不改播放备注和更新时间。
func syncOwnedFilmTx(tx *gorm.DB, existing *model.FilmIndex, sourceID string, detail model.MovieDetail) (bool, error) {
	changed, keys, err := syncOwnedFilmFieldsTx(tx, existing, sourceID, detail)
	if err != nil || keys == nil {
		return changed, err
	}
	if err := shared.SaveMovieMatchKeysByMidTx(tx, map[int64][]string{existing.Mid: keys}); err != nil {
		return false, err
	}
	return changed, nil
}

// syncOwnedFilmFieldsTx 只更新档案字段。keys 为 nil 表示不用重写匹配键；空切片表示要写成没有键。
func syncOwnedFilmFieldsTx(tx *gorm.DB, existing *model.FilmIndex, sourceID string, detail model.MovieDetail) (bool, []string, error) {
	if existing == nil || existing.Mid <= 0 || existing.FirstSourceId != sourceID {
		return false, nil, nil
	}
	if detail.Pid <= 0 && existing.Pid > 0 {
		detail.Pid = existing.Pid
	}
	if detail.Cid <= 0 && existing.Cid > 0 {
		detail.Cid = existing.Cid
	}
	if detail.CName == "" && existing.CName != "" {
		detail.CName = existing.CName
	}
	next, err := ConvertFilmIndex(sourceID, detail, taxonomy.GetCategoryVersion(), taxonomy.GetRuleVersion())
	if err != nil {
		return false, nil, err
	}
	updates := ownedFilmUpdates(existing, &next)
	changed := len(updates) > 0
	if changed {
		updates["updated_at"] = time.Now()
		if err := tx.Model(&model.FilmIndex{}).Where("mid = ?", existing.Mid).Updates(updates).Error; err != nil {
			return false, nil, err
		}
	}
	return changed, ownedMatchKeys(existing, updates, detail), nil
}

func ownedFilmUpdates(existing, next *model.FilmIndex) map[string]any {
	updates := make(map[string]any)
	locked := model.LockedColumns(existing.LockedFields)
	if existing.IsCustomPicture {
		locked["picture"] = struct{}{}
		locked["picture_slide"] = struct{}{}
	}
	putPresentInt(updates, "db_id", existing.DbId, next.DbId)
	putPresentInt(updates, "cid", existing.Cid, next.Cid)
	putPresentInt(updates, "pid", existing.Pid, next.Pid)
	putPresentString(updates, "root_category_key", existing.RootCategoryKey, next.RootCategoryKey)
	putPresentString(updates, "category_key", existing.CategoryKey, next.CategoryKey)
	putPresentString(updates, "original_category", existing.OriginalCategory, next.OriginalCategory)
	putPresentString(updates, "c_name", existing.CName, next.CName)
	putPresentString(updates, "series_key", existing.SeriesKey, next.SeriesKey)
	putPresentString(updates, "name", existing.Name, next.Name)
	putUnlocked(updates, locked, "sub_title", existing.SubTitle, next.SubTitle)
	putUnlocked(updates, locked, "class_tag", existing.ClassTag, next.ClassTag)
	putDiff(updates, "area", existing.Area, next.Area)
	putDiff(updates, "language", existing.Language, next.Language)
	putUnlocked(updates, locked, "year", existing.Year, next.Year)
	putDiff(updates, "initial", existing.Initial, next.Initial)
	putUnlocked(updates, locked, "score", existing.Score, next.Score)
	putDiff(updates, "hits", existing.Hits, next.Hits)
	putDiff(updates, "state", existing.State, next.State)
	putUnlocked(updates, locked, "actor", existing.Actor, next.Actor)
	putUnlocked(updates, locked, "director", existing.Director, next.Director)
	putDiff(updates, "writer", existing.Writer, next.Writer)
	putUnlocked(updates, locked, "blurb", existing.Blurb, next.Blurb)
	putUnlocked(updates, locked, "content", existing.Content, next.Content)
	putUnlocked(updates, locked, "release_date", existing.ReleaseDate, next.ReleaseDate)
	putPresentUnlocked(updates, locked, "picture", existing.Picture, next.Picture)
	putPresentUnlocked(updates, locked, "picture_slide", existing.PictureSlide, next.PictureSlide)
	return updates
}

// 豆瓣、片名、分类、封面只在新值非空时覆盖。列表缺字段时不能把已有值写成空。
func putPresentInt(updates map[string]any, column string, oldValue, nextValue int64) {
	if nextValue <= 0 || oldValue == nextValue {
		return
	}
	updates[column] = nextValue
}

func putPresentString(updates map[string]any, column, oldValue, nextValue string) {
	if strings.TrimSpace(nextValue) == "" || oldValue == nextValue {
		return
	}
	updates[column] = nextValue
}

func putPresentUnlocked(updates map[string]any, locked map[string]struct{}, column, oldValue, nextValue string) {
	if _, skip := locked[column]; skip {
		return
	}
	putPresentString(updates, column, oldValue, nextValue)
}

func ownedMatchKeys(existing *model.FilmIndex, updates map[string]any, detail model.MovieDetail) []string {
	if existing == nil || existing.Mid <= 0 {
		return nil
	}
	dbID := existing.DbId
	name := existing.Name
	pid := existing.Pid
	if v, ok := updates["db_id"].(int64); ok && v > 0 {
		dbID = v
	}
	if v, ok := updates["name"].(string); ok && strings.TrimSpace(v) != "" {
		name = v
	}
	if v, ok := updates["pid"].(int64); ok && v > 0 {
		pid = v
	}
	if pid <= 0 {
		pid = detail.RawPid
	}
	if pid <= 0 {
		pid = detail.Pid
	}
	keys := shared.BuildMovieMatchKeysWithCategory(dbID, name, pid)
	if keys == nil {
		return []string{}
	}
	return keys
}

func putUnlocked[T comparable](updates map[string]any, locked map[string]struct{}, column string, oldValue, nextValue T) {
	if _, skip := locked[column]; skip {
		return
	}
	putDiff(updates, column, oldValue, nextValue)
}

func putDiff[T comparable](updates map[string]any, column string, oldValue, nextValue T) {
	if oldValue != nextValue {
		updates[column] = nextValue
	}
}

func runPeerCollectTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	conn := db.Mdb
	if conn == nil {
		return errors.New("数据库未连接")
	}
	if ctx != nil {
		conn = conn.WithContext(ctx)
	}
	if conn.Dialector != nil && conn.Dialector.Name() == "mysql" {
		return conn.Transaction(fn, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	}
	return conn.Transaction(fn)
}

func saveSinglePeerDetailTx(source *model.FilmSource, detail model.MovieDetail) (mid int64, isNew bool, playLinesChanged bool, stored bool, err error) {
	return saveSinglePeerDetailTxCtx(context.Background(), source, detail)
}

func saveSinglePeerDetailTxCtx(ctx context.Context, source *model.FilmSource, detail model.MovieDetail) (mid int64, isNew bool, playLinesChanged bool, stored bool, err error) {
	if source != nil {
		prepareSourceOtherCategory(source.Id)
	}
	var linesChanged bool
	var metaChanged bool
	err = runPeerCollectTxWithRetry(ctx, func(tx *gorm.DB) error {
		mid, isNew, playLinesChanged, linesChanged, metaChanged = 0, false, false, false, false
		var existingMapping model.MovieSourceMapping
		hasMapping := false
		if err := tx.Where("source_id = ? AND source_mid = ?", source.Id, detail.Id).First(&existingMapping).Error; err == nil && existingMapping.GlobalMid > 0 {
			hasMapping = true
			mid = existingMapping.GlobalMid
		}

		var filmIndex *model.FilmIndex
		if hasMapping {
			var fi model.FilmIndex
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Select(ownedFilmSyncSelect).
				Where("mid = ?", mid).
				First(&fi).Error; err == nil {
				filmIndex = &fi
			}
		}

		if filmIndex != nil {
			if filmIndex.FirstSourceId != source.Id {
				filmIndex = nil
				mid = 0
			}
		} else {
			mid = 0
		}

		if filmIndex != nil {
			keep, err := vodKeepsMappedFilmTx(tx, source.Id, detail.Id, filmIndex.Mid)
			if err != nil {
				return err
			}
			if !keep {
				filmIndex = nil
				mid = 0
			}
		}

		won, err := claimSourceVodTimeTx(tx, source.Id, detail.Id, detail.UpdateTime)
		if err != nil {
			return err
		}
		if !won {
			mid = 0
			isNew = false
			return nil
		}
		if filmIndex == nil {
			if !shared.HasPlayableLinks(detail) {
				return errors.New("影片没有有效播放链接，不能建档")
			}
			fi, err := createNewFilmIndexTx(tx, source.Id, detail)
			if err != nil {
				return err
			}
			mid = fi.Mid
			filmIndex = fi
			isNew = true

			if err := shared.SaveMovieSourceMappingTx(tx, source.Id, detail.Id, mid); err != nil {
				return err
			}
			if _, err := claimSourceVodTimeTx(tx, source.Id, detail.Id, detail.UpdateTime); err != nil {
				return err
			}
		}
		newLines := buildPlaylistsFromDetail(mid, source.Id, detail)
		var playChanged bool
		linesChanged, playChanged, err = saveStationPlaylistsTx(tx, mid, source.Id, newLines)
		if err != nil {
			return err
		}
		playLinesChanged = playChanged

		if !isNew {
			metaChanged, err = syncOwnedFilmTx(tx, filmIndex, source.Id, detail)
			if err != nil {
				return err
			}
		}
		if linesChanged || isNew {
			sources := sourcecatalog.GetCollectSourceListTx(tx)
			var stampMids map[int64]struct{}
			if !isNew && playChanged {
				stampMids = map[int64]struct{}{mid: {}}
			}
			if err := refreshRemarksAndPlaySummaryWithStampTx(tx, []int64{mid}, stampMids, sources); err != nil {
				return err
			}
		}

		return nil
	})
	stored = isNew || linesChanged || metaChanged
	return mid, isNew, playLinesChanged, stored, err
}

// writePeerPage 和 savePeerFilm 默认走批量事务和单片事务。测试可替换它们。
var (
	writePeerPage           = writePeerPageTx
	defaultWritePeerPagePtr = reflect.ValueOf(writePeerPageTx).Pointer()
	savePeerFilm            = saveSinglePeerDetailTx
	defaultSavePeerFilmPtr  = reflect.ValueOf(saveSinglePeerDetailTx).Pointer()
)

// SaveCollectedPeerDetails 全站平权影片采集入库入口。
// 正常仍是一页一个事务。先无锁只读预检剔除无变化与过期片目，再按 (source_id, source_mid) 分片加锁写入。
func SaveCollectedPeerDetails(
	ctx context.Context,
	source *model.FilmSource,
	page int,
	list []model.MovieDetail,
) (model.Mids, error) {
	details := filterPeerPageDetails(list)
	if len(details) == 0 {
		return model.Mids{}, nil
	}
	if source == nil {
		return model.Mids{}, errNilCollectSource
	}
	if reflect.ValueOf(writePeerPage).Pointer() == defaultWritePeerPagePtr && db.Mdb != nil {
		filtered, preErr := preflightFilterUnchangedDetails(ctx, db.Mdb, source, details)
		if preErr == nil {
			details = filtered
		}
		if len(details) == 0 {
			return model.Mids{}, nil
		}
	}
	prepareSourceOtherCategory(source.Id)
	sources := sourcecatalog.GetCollectSourceList()
	unlock := lockPeerCollectPage(source, details)
	defer unlock()

	var affected []int64
	var notify []int64
	var unmapped int
	pageCtx, timing := peerTimingContext(ctx)
	started := time.Now()
	err := runPeerCollectTxWithRetry(pageCtx, func(tx *gorm.DB) error {
		tx = tx.WithContext(pageCtx)
		var writeErr error
		affected, notify, unmapped, writeErr = writePeerPage(tx, source, details, sources)
		return writeErr
	})
	if page > 0 && page%50 == 0 {
		log.Printf("[Spider][PeerCollect] 页耗时 source=%s page=%d path=%s map_create=%s playlist=%s remarks=%s tx=%s err=%v",
			source.Id, page, timing.path, timing.mapCost, timing.playlist, timing.remarks, time.Since(started), err)
	}
	if err == nil {
		if unmapped > 0 {
			log.Printf("[Spider][PeerCollect] 分类跳过 source=%s page=%d unmapped=%d",
				source.Id, page, unmapped)
		}
		return model.Mids{
			Affected: uniqueMIDs(affected),
			Notify:   uniqueMIDs(notify),
			Unmapped: unmapped,
		}, nil
	}
	if isDeterministicRowError(err) {
		log.Printf("[Spider][PeerCollect] 整页因字段超长回滚，改为逐片入库 source=%s page=%d vods=%d err=%v",
			source.Id, page, len(details), err)
		return savePeerFilmsIsolated(pageCtx, source, page, details)
	}
	log.Printf("[Spider][PeerCollect] 整页入库失败 source=%s page=%d vods=%d err=%v",
		source.Id, page, len(details), err)
	return model.Mids{}, err
}
