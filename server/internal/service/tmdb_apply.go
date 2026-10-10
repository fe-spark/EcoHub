package service

import (
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
)

var isoDateRegex = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// ApplyDetail 将 TMDB 刮削出的元数据应用到指定影片并落库刷新
func (s *TMDBService) ApplyDetail(req model.TMDBApplyReq) error {
	_, err := s.ApplyDetailWithOptions(req, true)
	return err
}

// ApplyDetailWithOptions 将 TMDB 刮削出的元数据应用到指定影片并落库，支持按需控制是否立即增量发布快照
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

	fieldSet := make(map[string]struct{}, len(req.Fields))
	for _, f := range req.Fields {
		fieldSet[strings.ToLower(strings.TrimSpace(f))] = struct{}{}
	}
	applyAll := len(fieldSet) == 0

	shouldApply := func(names ...string) bool {
		if applyAll {
			return true
		}
		for _, name := range names {
			if _, ok := fieldSet[strings.ToLower(name)]; ok {
				return true
			}
		}
		return false
	}

	updates := make(map[string]any)

	// 1. 竖版海报（锁定保护）
	if shouldApply("poster", "picture") && tmdbData.Poster != "" {
		updates["picture"] = tmdbData.Poster
		updates["custom_picture"] = tmdbData.Poster
		updates["is_custom_picture"] = true
	}
	// 2. 横版幻灯图（仅写横图，不设置 is_custom_picture，避免排片刮削把片库封面锁死）
	if shouldApply("backdrop", "pictureslide") && tmdbData.Backdrop != "" {
		updates["picture_slide"] = tmdbData.Backdrop
		updates["custom_picture_slide"] = tmdbData.Backdrop
	}
	// 3. 剧情简介与摘要
	if shouldApply("overview", "content") && tmdbData.Overview != "" {
		updates["content"] = tmdbData.Overview
		updates["blurb"] = tmdbData.Overview
	}
	// 4. 子标题/原始片名
	if shouldApply("subtitle", "originaltitle") && tmdbData.OriginalTitle != "" {
		updates["sub_title"] = tmdbData.OriginalTitle
	}
	// 5. 演员
	if shouldApply("actor", "credits") && len(tmdbData.Actors) > 0 {
		updates["actor"] = strings.Join(tmdbData.Actors, "/")
	}
	// 6. 导演
	if shouldApply("director", "credits") && len(tmdbData.Directors) > 0 {
		updates["director"] = strings.Join(tmdbData.Directors, "/")
	}
	// 7. 年份与上映日期
	if shouldApply("year", "releasedate") {
		if tmdbData.Year != "" {
			if y, parseErr := strconv.ParseInt(strings.TrimSpace(tmdbData.Year), 10, 64); parseErr == nil && y > 0 {
				updates["year"] = y
			}
		}
		if tmdbData.ReleaseDate != "" {
			cleanDate := strings.TrimSpace(tmdbData.ReleaseDate)
			if match := isoDateRegex.FindString(cleanDate); match != "" {
				updates["release_date"] = match
				if _, hasYear := updates["year"]; !hasYear {
					if y, parseErr := strconv.ParseInt(match[:4], 10, 64); parseErr == nil && y > 0 {
						updates["year"] = y
					}
				}
			} else {
				updates["release_date"] = cleanDate
			}
		}
	}
	// 8. 评分
	if shouldApply("score", "voteaverage") && tmdbData.VoteScore != "" {
		if score, parseErr := strconv.ParseFloat(strings.TrimSpace(tmdbData.VoteScore), 64); parseErr == nil && score >= 0 {
			updates["score"] = score
		}
	}
	// 9. 分类标签
	if shouldApply("tag", "genres") && len(tmdbData.Genres) > 0 {
		updates["class_tag"] = strings.Join(tmdbData.Genres, ",")
	}

	if len(updates) == 0 {
		return req.Mid, nil
	}

	updates["update_reason"] = "TMDB刮削"
	updates["update_stamp"] = time.Now().Unix()

	// 精准局部更新，绝对不覆盖分类及来源标识字段
	if err := db.Mdb.Model(&model.FilmIndex{}).Where("mid = ?", req.Mid).Updates(updates).Error; err != nil {
		return 0, fmt.Errorf("更新影片元数据失败: %w", err)
	}

	// 增量同步检索标签（若修改了标签或年份）
	if _, hasTag := updates["class_tag"]; hasTag {
		_ = writer.UpsertSearchTagsByMids(req.Mid)
	} else if _, hasYear := updates["year"]; hasYear {
		_ = writer.UpsertSearchTagsByMids(req.Mid)
	}

	// 清理分类列表缓存
	if indexRec.Pid > 0 {
		writer.ClearFilmIndexCachesByPidSet(map[int64]struct{}{indexRec.Pid: {}})
	}

	// 增量发布快照并清理播放详情缓存
	if publishSnapshot {
		_, _, _ = filmsnapshot.UpsertActiveSnapshotsByMids(req.Mid)
		filmsnapshot.ClearDynamicPlayCaches()
	}

	return req.Mid, nil
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
