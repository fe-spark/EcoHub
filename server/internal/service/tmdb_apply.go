package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/film/writer"
	"server/internal/repository/support"

	"gorm.io/gorm/clause"
)

var isoDateRegex = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// ApplyDetail 将 TMDB 刮削出的元数据保存至 film_tmdb_scrapes 独立刮削表并刷新快照
func (s *TMDBService) ApplyDetail(req model.TMDBApplyReq) error {
	_, err := s.ApplyDetailWithOptions(req, true)
	return err
}

// ApplyDetailWithOptions 将 TMDB 刮削出的元数据保存至 film_tmdb_scrapes 独立刮削表，支持按需控制是否立即增量发布快照。
// 物理分层：采集站原始数据完整保留在 film_index，TMDB 刮削数据保存在 film_tmdb_scrapes，绝不篡改底层原始数据。
func (s *TMDBService) ApplyDetailWithOptions(req model.TMDBApplyReq, publishSnapshot bool) (int64, error) {
	if req.Mid <= 0 || req.TmdbID <= 0 {
		return 0, errors.New("影片ID或 TMDB ID 参数非法")
	}

	tmdbData, err := s.FetchDetail(req.TmdbID, req.MediaType)
	if err != nil {
		return 0, fmt.Errorf("获取 TMDB 详情失败: %w", err)
	}

	var indexRec model.FilmIndex
	if err := db.Mdb.Where("mid = ?", req.Mid).First(&indexRec).Error; err != nil {
		return 0, fmt.Errorf("未找到对应的影片信息 (mid=%d): %w", req.Mid, err)
	}

	var yearVal int64
	if tmdbData.Year != "" {
		if y, parseErr := strconv.ParseInt(strings.TrimSpace(tmdbData.Year), 10, 64); parseErr == nil && y > 0 {
			yearVal = y
		}
	}
	releaseDateVal := strings.TrimSpace(tmdbData.ReleaseDate)
	if match := isoDateRegex.FindString(releaseDateVal); match != "" {
		releaseDateVal = match
		if yearVal <= 0 {
			if y, parseErr := strconv.ParseInt(match[:4], 10, 64); parseErr == nil && y > 0 {
				yearVal = y
			}
		}
	}

	var scoreVal float64
	if tmdbData.VoteScore != "" {
		if score, parseErr := strconv.ParseFloat(strings.TrimSpace(tmdbData.VoteScore), 64); parseErr == nil && score >= 0 {
			scoreVal = score
		}
	}
	if scoreVal <= 0 && tmdbData.VoteAverage > 0 {
		scoreVal = tmdbData.VoteAverage
	}

	rawBytes, _ := json.Marshal(tmdbData)

	scrapeRecord := model.FilmTMDBScrape{
		Mid:           req.Mid,
		TmdbID:        req.TmdbID,
		MediaType:     req.MediaType,
		Title:         tmdbData.Title,
		OriginalTitle: tmdbData.OriginalTitle,
		ReleaseDate:   releaseDateVal,
		Year:          yearVal,
		Poster:        tmdbData.Poster,
		Backdrop:      tmdbData.Backdrop,
		VoteAverage:   scoreVal,
		VoteScore:     tmdbData.VoteScore,
		Overview:      tmdbData.Overview,
		Tagline:       tmdbData.Tagline,
		Genres:        strings.Join(tmdbData.Genres, ","),
		Directors:     strings.Join(tmdbData.Directors, "/"),
		Actors:        strings.Join(tmdbData.Actors, "/"),
		AppliedFields: strings.Join(req.Fields, ","),
		RawJSON:       string(rawBytes),
		Status:        1,
		UpdatedAt:     time.Now(),
	}

	if err := db.Mdb.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(&scrapeRecord).Error; err != nil {
		return 0, fmt.Errorf("保存 TMDB 刮削数据失败: %w", err)
	}

	// 增量发布快照并清理播放详情缓存
	if publishSnapshot {
		_, _, _ = filmsnapshot.UpsertActiveSnapshotsByMids(req.Mid)
		filmsnapshot.ClearDynamicPlayCaches()
		support.ClearIndexPageCache()
	}

	// 清理分类列表缓存
	if indexRec.Pid > 0 {
		writer.ClearFilmIndexCachesByPidSet(map[int64]struct{}{indexRec.Pid: {}})
	}

	return req.Mid, nil
}

// RevertDetails 还原影片的 TMDB 刮削数据（支持单个与批量），恢复为采集站原始数据
func (s *TMDBService) RevertDetails(mids []int64) error {
	if len(mids) == 0 {
		return nil
	}

	cleanMids := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, id := range mids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleanMids = append(cleanMids, id)
	}
	if len(cleanMids) == 0 {
		return nil
	}

	if err := db.Mdb.Where("mid IN ?", cleanMids).Delete(&model.FilmTMDBScrape{}).Error; err != nil {
		return fmt.Errorf("还原刮削失败: %w", err)
	}

	// 增量发布快照并清理播放详情缓存
	_, _, _ = filmsnapshot.UpsertActiveSnapshotsByMids(cleanMids...)
	filmsnapshot.ClearDynamicPlayCaches()
	support.ClearIndexPageCache()

	var indexRecs []model.FilmIndex
	if err := db.Mdb.Where("mid IN ?", cleanMids).Find(&indexRecs).Error; err == nil {
		pids := make(map[int64]struct{})
		for _, rec := range indexRecs {
			if rec.Pid > 0 {
				pids[rec.Pid] = struct{}{}
			}
		}
		if len(pids) > 0 {
			writer.ClearFilmIndexCachesByPidSet(pids)
		}
	}

	return nil
}

// RevertDetail 还原单个影片的 TMDB 刮削数据
func (s *TMDBService) RevertDetail(mid int64) error {
	return s.RevertDetails([]int64{mid})
}

// FetchFormPrefill 拉取用于前端表单自动填充的元数据
func (s *TMDBService) FetchFormPrefill(tmdbID int64, mediaType string) (map[string]any, error) {
	d, err := s.FetchDetail(tmdbID, mediaType)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"name":         d.Title,
		"subTitle":     d.OriginalTitle,
		"picture":      d.Poster,
		"pictureSlide": d.Backdrop,
		"content":      d.Overview,
		"actor":        strings.Join(d.Actors, "/"),
		"director":     strings.Join(d.Directors, "/"),
		"year":         d.Year,
		"releaseDate":  d.ReleaseDate,
		"dbScore":      d.VoteScore,
		"classTag":     strings.Join(d.Genres, ","),
	}
	return result, nil
}
