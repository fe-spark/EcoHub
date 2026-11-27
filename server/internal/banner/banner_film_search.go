package banner

import (
	"context"
	"math"
	"strings"

	filmshared "server/internal/film/shared"
	"server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
)

// BannerFilmOption 轮播选片弹窗使用的采集站影片摘要。
type BannerFilmOption struct {
	Mid          int64   `json:"id"`
	Name         string  `json:"name"`
	CName        string  `json:"cName"`
	Year         int64   `json:"year"`
	Remarks      string  `json:"remarks"`
	Picture      string  `json:"picture"`
	PictureSlide string  `json:"pictureSlide"`
	Area         string  `json:"area"`
	Director     string  `json:"director"`
	Actor        string  `json:"actor"`
	Score        float64 `json:"score"`
	Hits         int64   `json:"hits"`
	IsScraped    bool    `json:"isScraped"`
}

func escapeBannerLikeNeedle(s string) string {
	s = strings.ReplaceAll(s, "!", "!!")
	s = strings.ReplaceAll(s, "%", "!%")
	s = strings.ReplaceAll(s, "_", "!_")
	return "%" + s + "%"
}

func bannerFilmOrderClause(sortField string, hasKeyword bool) string {
	switch strings.TrimSpace(sortField) {
	case "update_stamp", "latest":
		return "update_stamp DESC, mid DESC"
	case "hits":
		return "hits DESC, mid DESC"
	case "score":
		return "score DESC, hits DESC, mid DESC"
	case "year":
		return "year DESC, update_stamp DESC, mid DESC"
	default:
		if hasKeyword {
			return "hits DESC, update_stamp DESC, mid DESC"
		}
		return "update_stamp DESC, mid DESC"
	}
}

// SearchBannerFilmOptions 按指定采集站直查可供轮播选取的影片列表（需在该站有播放线路）。
func SearchBannerFilmOptions(ctx context.Context, sourceID, keyword, sortField string, page *dto.Page) ([]BannerFilmOption, error) {
	sourceID = strings.TrimSpace(sourceID)
	keyword = strings.TrimSpace(keyword)
	if page == nil {
		page = &dto.Page{Current: 1, PageSize: 10}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	if page.PageSize <= 0 {
		page.PageSize = 10
	} else if page.PageSize > 50 {
		page.PageSize = 50
	}
	if sourceID == "" || db.Mdb == nil {
		page.Total = 0
		page.PageCount = 0
		return []BannerFilmOption{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 仅做乘法防溢出，不额外引入人为总页数截断分支。
	offsetOverflow := page.Current-1 > math.MaxInt/page.PageSize
	offset := math.MaxInt
	if !offsetOverflow {
		offset = (page.Current - 1) * page.PageSize
	}
	var mids []int64
	handledInMemory := false
	if keyword == "" {
		if fastMids, total, ok := snapshot.PagePrimarySourceFilmMids(sourceID, sortField, offset, page.PageSize); ok {
			handledInMemory = true
			page.Total = total
			if total == 0 {
				page.PageCount = 0
				return []BannerFilmOption{}, nil
			}
			page.PageCount = (total + page.PageSize - 1) / page.PageSize
			mids = fastMids
		}
	} else {
		if fastMids, total, ok := snapshot.SearchPrimarySourceFilmMids(sourceID, keyword, sortField, offset, page.PageSize); ok {
			handledInMemory = true
			page.Total = total
			page.PageCount = (total + page.PageSize - 1) / page.PageSize
			mids = fastMids
		}
	}

	const selectCols = "mid, first_source_id, db_id, pid, name, c_name, year, remarks, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, area, director, actor, score, hits"
	type row struct {
		Mid                int64
		FirstSourceId      string
		DbId               int64
		Pid                int64
		Name               string
		CName              string
		Year               int64
		Remarks            string
		Picture            string
		PictureSlide       string
		CustomPicture      string
		CustomPictureSlide string
		IsCustomPicture    bool
		Area               string
		Director           string
		Actor              string
		Score              float64
		Hits               int64
	}
	var rows []row

	if !handledInMemory {
		query := db.Mdb.WithContext(ctx).Model(&model.FilmIndex{}).
			Where(model.FilmHasPlaySourceSQL(), sourceID, model.LineKindPlay)
		if keyword != "" {
			if len([]rune(keyword)) < 2 || offsetOverflow {
				page.Total = 0
				page.PageCount = 0
				return []BannerFilmOption{}, nil
			}
			needle := escapeBannerLikeNeedle(keyword)
			query = query.Where(
				"(name LIKE ? ESCAPE '!' OR sub_title LIKE ? ESCAPE '!' OR actor LIKE ? ESCAPE '!' OR director LIKE ? ESCAPE '!')",
				needle, needle, needle, needle,
			)
			// 带 4 列 LIKE 搜索避免全表 COUNT(*)，多取 1 条探测是否有下一页。
			var probed []int64
			if err := query.
				Select("mid").
				Order(bannerFilmOrderClause(sortField, true)).
				Offset(offset).
				Limit(page.PageSize + 1).
				Pluck("mid", &probed).Error; err != nil {
				return nil, err
			}
			hasMore := len(probed) > page.PageSize
			if hasMore {
				mids = probed[:page.PageSize]
				page.Total = offset + len(mids) + 1
				page.PageCount = page.Current + 1
			} else {
				mids = probed
				page.Total = offset + len(mids)
				if page.Total == 0 {
					page.PageCount = 0
				} else {
					page.PageCount = page.Current
				}
			}
		} else {
			if err := dto.GetPage(query, page); err != nil {
				return nil, err
			}
			if page.Total == 0 || offsetOverflow || offset >= page.Total {
				return []BannerFilmOption{}, nil
			}
			// 延迟关联：先按排序取当前页 mid，再按主键批量回表取展示字段，避免深分页排序携带大字段。
			if err := query.
				Select("mid").
				Order(bannerFilmOrderClause(sortField, false)).
				Offset(offset).
				Limit(page.PageSize).
				Pluck("mid", &mids).Error; err != nil {
				return nil, err
			}
		}
	}
	if len(mids) == 0 {
		return []BannerFilmOption{}, nil
	}
	if err := db.Mdb.WithContext(ctx).Model(&model.FilmIndex{}).
		Select(selectCols).
		Where("mid IN ?", mids).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	byMid := make(map[int64]row, len(rows))
	for _, r := range rows {
		byMid[r.Mid] = r
	}
	orderedRows := make([]row, 0, len(mids))
	for _, id := range mids {
		if r, ok := byMid[id]; ok {
			orderedRows = append(orderedRows, r)
		}
	}
	rows = orderedRows

	rowMids := make([]int64, 0, len(rows))
	posters := make([]model.FilmListSnapshot, 0, len(rows))
	for _, r := range rows {
		rowMids = append(rowMids, r.Mid)
		pic := strings.TrimSpace(r.Picture)
		if r.IsCustomPicture && strings.TrimSpace(r.CustomPicture) != "" {
			pic = strings.TrimSpace(r.CustomPicture)
		}
		slide := strings.TrimSpace(r.PictureSlide)
		if r.IsCustomPicture && strings.TrimSpace(r.CustomPictureSlide) != "" {
			slide = strings.TrimSpace(r.CustomPictureSlide)
		}
		posters = append(posters, model.FilmListSnapshot{
			Mid:             r.Mid,
			SourceId:        r.FirstSourceId,
			DbId:            r.DbId,
			Name:            r.Name,
			Pid:             r.Pid,
			Picture:         pic,
			PictureSlide:    slide,
			IsCustomPicture: r.IsCustomPicture,
		})
	}

	posterByMid := make(map[int64]model.FilmListSnapshot, len(posters))
	for _, snap := range filmshared.ApplyPosterSourcePictures(posters) {
		posterByMid[snap.Mid] = snap
	}

	scrapedSet := make(map[int64]bool, len(rowMids))
	if len(rowMids) > 0 {
		var scrapeMids []int64
		if err := db.Mdb.WithContext(ctx).Model(&model.FilmTMDBScrape{}).
			Where("mid IN ? AND tmdb_id > 0", rowMids).
			Pluck("mid", &scrapeMids).Error; err == nil {
			for _, mid := range scrapeMids {
				scrapedSet[mid] = true
			}
		}
	}

	out := make([]BannerFilmOption, 0, len(rows))
	for _, r := range rows {
		pic := strings.TrimSpace(r.Picture)
		if r.IsCustomPicture && strings.TrimSpace(r.CustomPicture) != "" {
			pic = strings.TrimSpace(r.CustomPicture)
		}
		slide := strings.TrimSpace(r.PictureSlide)
		if r.IsCustomPicture && strings.TrimSpace(r.CustomPictureSlide) != "" {
			slide = strings.TrimSpace(r.CustomPictureSlide)
		}
		if snap, ok := posterByMid[r.Mid]; ok {
			pic = strings.TrimSpace(snap.DisplayPicture())
			slide = strings.TrimSpace(snap.DisplayPictureSlide())
		}
		if slide == pic {
			slide = ""
		}
		out = append(out, BannerFilmOption{
			Mid:          r.Mid,
			Name:         strings.TrimSpace(r.Name),
			CName:        strings.TrimSpace(r.CName),
			Year:         r.Year,
			Remarks:      strings.TrimSpace(r.Remarks),
			Picture:      pic,
			PictureSlide: slide,
			Area:         strings.TrimSpace(r.Area),
			Director:     strings.TrimSpace(r.Director),
			Actor:        strings.TrimSpace(r.Actor),
			Score:        r.Score,
			Hits:         r.Hits,
			IsScraped:    scrapedSet[r.Mid],
		})
	}
	return out, nil
}
