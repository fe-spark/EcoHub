package writer

import (
	"context"
	"database/sql"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/shared"
	"server/internal/repository/film/snapshot"
	"server/internal/repository/support"
	"server/internal/spider/scheduler"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func createNewFilmIndexTx(tx *gorm.DB, sourceID string, detail model.MovieDetail) (*model.FilmIndex, error) {
	categoryVersion := support.GetCategoryVersion()
	ruleVersion := support.GetRuleVersion()
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
func syncOwnedFilmTx(tx *gorm.DB, existing *model.FilmIndex, sourceID string, detail model.MovieDetail) error {
	if existing == nil || existing.Mid <= 0 || existing.FirstSourceId != sourceID {
		return nil
	}
	next, err := ConvertFilmIndex(sourceID, detail, support.GetCategoryVersion(), support.GetRuleVersion())
	if err != nil {
		return err
	}
	updates := ownedFilmUpdates(existing, &next)
	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
		if err := tx.Model(&model.FilmIndex{}).Where("mid = ?", existing.Mid).Updates(updates).Error; err != nil {
			return err
		}
	}
	return rewriteOwnedMatchKeysTx(tx, existing, updates, detail)
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

// rewriteOwnedMatchKeysTx 按更新后的豆瓣、片名、大类重写本片匹配键，并删掉不再成立的旧键。
func rewriteOwnedMatchKeysTx(tx *gorm.DB, existing *model.FilmIndex, updates map[string]any, detail model.MovieDetail) error {
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
	return shared.SaveMovieMatchKeysByMidTx(tx, map[int64][]string{existing.Mid: keys})
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

func refreshRemarksAndPlaySummaryTx(tx *gorm.DB, mid int64) error {
	if mid <= 0 {
		return nil
	}
	return refreshRemarksAndPlaySummaryWithStampTx(tx, []int64{mid}, map[int64]struct{}{mid: {}}, support.GetCollectSourceListTx(tx))
}

func pickPlaylistRemarks(playLines []model.FilmSourcePlaylist, sources []model.FilmSource) string {
	if len(playLines) == 0 {
		return ""
	}
	orderByID := make(map[string]int, len(sources))
	enabledByID := make(map[string]bool, len(sources))
	for idx, source := range sources {
		orderByID[source.Id] = idx
		if source.State {
			enabledByID[source.Id] = true
		}
	}
	candidates := playLines
	if len(enabledByID) > 0 {
		enabledLines := make([]model.FilmSourcePlaylist, 0, len(playLines))
		for _, line := range playLines {
			if enabledByID[line.SourceId] {
				enabledLines = append(enabledLines, line)
			}
		}
		if len(enabledLines) > 0 {
			candidates = enabledLines
		}
	}
	bestLine := candidates[0]
	for i := 1; i < len(candidates); i++ {
		curr := candidates[i]
		if curr.EpisodeCount > bestLine.EpisodeCount {
			bestLine = curr
			continue
		}
		if curr.EpisodeCount != bestLine.EpisodeCount {
			continue
		}
		oCurr := math.MaxInt
		if idx, ok := orderByID[curr.SourceId]; ok {
			oCurr = idx
		}
		oBest := math.MaxInt
		if idx, ok := orderByID[bestLine.SourceId]; ok {
			oBest = idx
		}
		if oCurr < oBest || (oCurr == oBest && curr.GroupIndex < bestLine.GroupIndex) {
			bestLine = curr
		}
	}
	return bestLine.LastEpisode
}

// refreshRemarksAndPlaySummaryWithStampTx 刷新备注与播放摘要；仅 stampMids 中的存量追更影片将 update_stamp 刷为当前采集时间，新片保留源站 vod_time。
func refreshRemarksAndPlaySummaryWithStampTx(tx *gorm.DB, mids []int64, stampMids map[int64]struct{}, sources []model.FilmSource) error {
	ordered := uniqueMIDs(mids)
	if len(ordered) == 0 {
		return nil
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })

	var playLines []model.FilmSourcePlaylist
	if err := tx.Select("mid", "source_id", "line_kind", "group_index", "group_name", "episode_count", "last_episode").
		Where("mid IN ? AND line_kind = ?", ordered, "play").
		Find(&playLines).Error; err != nil {
		return err
	}
	byMid := make(map[int64][]model.FilmSourcePlaylist, len(ordered))
	for _, line := range playLines {
		byMid[line.Mid] = append(byMid[line.Mid], line)
	}
	now := time.Now()
	stamp := now.Unix()
	for _, mid := range ordered {
		cols := map[string]any{
			"remarks":           pickPlaylistRemarks(byMid[mid], sources),
			"play_from_summary": snapshot.BuildPlayFromSummaryFromPlaylists(byMid[mid], sources),
			"updated_at":        now,
		}
		if _, ok := stampMids[mid]; ok {
			cols["update_stamp"] = stamp
		}
		if err := tx.Model(&model.FilmIndex{}).Where("mid = ?", mid).Updates(cols).Error; err != nil {
			return err
		}
	}
	return nil
}

func runPeerCollectTx(fn func(tx *gorm.DB) error) error {
	if db.Mdb != nil && db.Mdb.Dialector != nil && db.Mdb.Dialector.Name() == "mysql" {
		return db.Mdb.Transaction(fn, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	}
	return db.Mdb.Transaction(fn)
}

func saveSinglePeerDetailTx(source *model.FilmSource, detail model.MovieDetail) (mid int64, isNew bool, playLinesChanged bool, err error) {
	err = runPeerCollectTx(func(tx *gorm.DB) error {
		var existingMapping model.MovieSourceMapping
		hasMapping := false
		if err := tx.Where("source_id = ? AND source_mid = ?", source.Id, detail.Id).First(&existingMapping).Error; err == nil && existingMapping.GlobalMid > 0 {
			hasMapping = true
			mid = existingMapping.GlobalMid
		}

		var filmIndex *model.FilmIndex
		if hasMapping {
			var fi model.FilmIndex
			if err := tx.Where("mid = ?", mid).First(&fi).Error; err == nil {
				filmIndex = &fi
			}
		}

		if filmIndex != nil && filmIndex.FirstSourceId != source.Id {
			filmIndex = nil
			mid = 0
		}

		if filmIndex == nil {
			claimedMid, claimedFilm, err := claimPeerFilmByMatchKeysTx(tx, source, detail)
			if err != nil {
				return err
			}
			if claimedFilm != nil {
				mid = claimedMid
				filmIndex = claimedFilm
				if err := shared.SaveMovieSourceMappingTx(tx, source.Id, detail.Id, mid); err != nil {
					return err
				}
			}
		}

		if filmIndex == nil {
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
		}

		newLines := buildPlaylistsFromDetail(mid, source.Id, detail)
		linesChanged, playChanged, err := saveStationPlaylistsTx(tx, mid, source.Id, newLines)
		if err != nil {
			return err
		}
		playLinesChanged = playChanged

		if !isNew {
			if err := syncOwnedFilmTx(tx, filmIndex, source.Id, detail); err != nil {
				return err
			}
		}
		if linesChanged || isNew {
			sources := support.GetCollectSourceListTx(tx)
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
	return mid, isNew, playLinesChanged, err
}

// writePeerPage 和 savePeerFilm 默认走批量事务和单片事务。测试可替换它们。
var (
	writePeerPage = writePeerPageTx
	savePeerFilm  = saveSinglePeerDetailTx
)

func isDeterministicRowError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Data too long") || strings.Contains(msg, "Error 1406")
}

// SaveCollectedPeerDetails 全站平权影片采集入库入口。
// 正常仍是一页一个事务。字段超长导致整页回滚时，改为逐片提交，只丢掉超长的那一部。
// 这类错误不返回给调用方，因此不会进入页级重试。连接失败等瞬时错误仍让整页重试。
func SaveCollectedPeerDetails(
	ctx context.Context,
	source *model.FilmSource,
	page int,
	list []model.MovieDetail,
) (scheduler.Mids, error) {
	_ = ctx
	details := filterPeerPageDetails(list)
	if len(details) == 0 {
		return scheduler.Mids{}, nil
	}
	if source == nil {
		return scheduler.Mids{}, errNilCollectSource
	}
	sources := support.GetCollectSourceList()
	unlock := lockPeerCollectPage(source, details)
	defer unlock()

	var affected []int64
	var notify []int64
	err := runPeerCollectTx(func(tx *gorm.DB) error {
		var writeErr error
		affected, notify, writeErr = writePeerPage(tx, source, details, sources)
		return writeErr
	})
	if err == nil {
		return scheduler.Mids{
			Affected: uniqueMIDs(affected),
			Notify:   uniqueMIDs(notify),
		}, nil
	}
	if isDeterministicRowError(err) {
		log.Printf("[Spider][PeerCollect] 整页因字段超长回滚，改为逐片入库 source=%s page=%d vods=%d err=%v",
			source.Id, page, len(details), err)
		return savePeerFilmsIsolated(source, page, details)
	}
	log.Printf("[Spider][PeerCollect] 整页入库失败 source=%s page=%d vods=%d err=%v",
		source.Id, page, len(details), err)
	return scheduler.Mids{}, err
}

func savePeerFilmsIsolated(source *model.FilmSource, page int, details []model.MovieDetail) (scheduler.Mids, error) {
	affected := make([]int64, 0, len(details))
	notify := make([]int64, 0, len(details))
	for _, detail := range details {
		mid, isNew, playChanged, err := savePeerFilm(source, detail)
		if err != nil {
			if isDeterministicRowError(err) {
				log.Printf("[Spider][PeerCollect] 单片入库跳过 source=%s page=%d vod=%d name=%s err=%v",
					source.Id, page, detail.Id, detail.Name, err)
				continue
			}
			log.Printf("[Spider][PeerCollect] 单片入库失败 source=%s page=%d vod=%d err=%v",
				source.Id, page, detail.Id, err)
			return scheduler.Mids{}, err
		}
		if mid <= 0 {
			continue
		}
		affected = append(affected, mid)
		if isNew || playChanged {
			notify = append(notify, mid)
		}
	}
	return scheduler.Mids{
		Affected: uniqueMIDs(affected),
		Notify:   uniqueMIDs(notify),
	}, nil
}

// SaveDetailsForCollect 兼容采集调度层调用。
func SaveDetailsForCollect(sourceID string, list []model.MovieDetail) (shared.CollectWriteResult, error) {
	source := repository.FindCollectSourceById(sourceID)
	if source == nil {
		source = &model.FilmSource{Id: sourceID, Name: sourceID, State: true}
	}
	mids, err := SaveCollectedPeerDetails(context.Background(), source, 1, list)
	if err != nil {
		return shared.CollectWriteResult{}, err
	}
	return shared.CollectWriteResult{
		AffectedMIDs: mids.Affected,
		NotifyMIDs:   mids.Notify,
	}, nil
}
