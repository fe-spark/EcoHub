package film

import (
	"fmt"

	"server/internal/model"
)

// ContentKey 前缀约定：
//   - vod_{源站vod_id}：主站身份键（与 FilmIndex.Mid 在单主站模型下数值相同，但语义是源站 ID，不是「系统 mid 字段名」）
//   - name_{hash}：无源站 ID 时的回退（hash 可能来自豆瓣身份或规范化片名，见 BuildMovieMatchKeys）
const (
	contentKeyVodPrefix  = "vod_"
	contentKeyNamePrefix = "name_"
)

// BuildContentKey 主站内容指纹，用于 film_index 唯一键与变更对比。
//
// 优先使用源站影片 ID（detail.Id / vod_id）：主站同一源下不同 vod 必须是不同条目。
// 仅靠规范化片名会把源站「并存且集数不同」的近似片名错误合并，例如：
//
//	烬九州第四季(145集, vod=87682) 与 烬九州：第四季(91集, vod=87676)
//	规范化后同为 烬九州#season4 → 同一 ContentKey → 每次采集互相覆盖 → 更新列表反复刷同一 mid。
//
// 豆瓣 ID / 规范化片名仍用于 movie_match_key 跨站匹配（BuildMovieMatchKeys），不作为主站主键。
// 无源站 ID 时回退 name_{hash}（手工录入等边缘场景）。
func BuildContentKey(detail model.MovieDetail) string {
	if detail.Id > 0 {
		return fmt.Sprintf("%s%d", contentKeyVodPrefix, detail.Id)
	}
	keys := BuildMovieMatchKeys(detail.DbId, detail.Name)
	if len(keys) == 0 {
		return ""
	}
	return contentKeyNamePrefix + keys[0]
}

func ApplyFilmIndex(detail *model.MovieDetail, info model.FilmIndex) {
	if detail == nil {
		return
	}
	detail.Id = info.Mid
	detail.Pid = info.Pid
	detail.Cid = info.Cid
	detail.Name = info.Name
	detail.SubTitle = info.SubTitle
	detail.CName = info.CName
	detail.ClassTag = info.ClassTag
	detail.Area = info.Area
	detail.Language = info.Language
	detail.State = info.State
	detail.Remarks = info.Remarks
	detail.Picture = info.Picture
	detail.PictureSlide = info.PictureSlide
	detail.Actor = info.Actor
	detail.Director = info.Director
	detail.Blurb = info.Blurb
	if info.Year > 0 {
		detail.Year = fmt.Sprint(info.Year)
	}
}

func ApplyFilmListSnapshot(detail *model.MovieDetail, info model.FilmListSnapshot) {
	if detail == nil {
		return
	}
	detail.Id = info.Mid
	detail.Pid = info.Pid
	detail.Cid = info.Cid
	detail.Name = info.Name
	detail.SubTitle = info.SubTitle
	detail.CName = info.CName
	detail.ClassTag = info.ClassTag
	detail.Area = info.Area
	detail.Language = info.Language
	detail.State = info.State
	detail.Remarks = info.Remarks
	detail.Picture = info.Picture
	detail.PictureSlide = info.PictureSlide
	detail.Actor = info.Actor
	detail.Director = info.Director
	detail.Blurb = info.Blurb
	if info.Year > 0 {
		detail.Year = fmt.Sprint(info.Year)
	}
}

func BuildMovieBasicInfos(infos ...model.FilmIndex) []model.MovieBasicInfo {
	list := make([]model.MovieBasicInfo, 0, len(infos))
	for _, s := range infos {
		list = append(list, model.MovieBasicInfo{
			Id:           s.Mid,
			Cid:          s.Cid,
			Pid:          s.Pid,
			Name:         s.Name,
			SubTitle:     s.SubTitle,
			CName:        s.CName,
			State:        s.State,
			Picture:      s.Picture,
			PictureSlide: s.PictureSlide,
			Actor:        s.Actor,
			Director:     s.Director,
			Blurb:        s.Blurb,
			Remarks:      s.Remarks,
			Area:         s.Area,
			Year:         fmt.Sprint(s.Year),
		})
	}
	return list
}

func BuildMovieBasicInfosFromSnapshots(infos ...model.FilmListSnapshot) []model.MovieBasicInfo {
	list := make([]model.MovieBasicInfo, 0, len(infos))
	for _, s := range infos {
		list = append(list, model.MovieBasicInfo{
			Id:           s.Mid,
			Cid:          s.Cid,
			Pid:          s.Pid,
			Name:         s.Name,
			SubTitle:     s.SubTitle,
			CName:        s.CName,
			State:        s.State,
			Picture:      s.Picture,
			PictureSlide: s.PictureSlide,
			Actor:        s.Actor,
			Director:     s.Director,
			Blurb:        s.Blurb,
			Remarks:      s.Remarks,
			Area:         s.Area,
			Year:         fmt.Sprint(s.Year),
		})
	}
	return list
}
