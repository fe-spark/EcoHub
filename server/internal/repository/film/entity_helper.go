package film

import (
	"fmt"

	"server/internal/model"
	"server/internal/utils"
)

func BuildContentKey(detail model.MovieDetail) string {
	// 生成内容指纹：优先使用豆瓣 ID，无豆瓣 ID 则使用名称哈希
	if detail.DbId != 0 {
		return fmt.Sprintf("dbid_%d", detail.DbId)
	}
	return fmt.Sprintf("name_%s", utils.GenerateHashKey(detail.Name))
}

func ApplyResolvedCategory(detail *model.MovieDetail, info model.SearchInfo) {
	if detail == nil {
		return
	}
	detail.Pid = info.Pid
	detail.Cid = info.Cid
}

func GetBasicInfoBySearchInfos(infos ...model.SearchInfo) []model.MovieBasicInfo {
	list := make([]model.MovieBasicInfo, 0, len(infos))
	for _, s := range infos {
		list = append(list, model.MovieBasicInfo{
			Id:       s.Mid,
			Cid:      s.Cid,
			Pid:      s.Pid,
			Name:     s.Name,
			SubTitle: s.SubTitle,
			CName:    s.CName,
			State:    s.State,
			Picture:  s.Picture,
			Actor:    s.Actor,
			Director: s.Director,
			Blurb:    s.Blurb,
			Remarks:  s.Remarks,
			Area:     s.Area,
			Year:     fmt.Sprint(s.Year),
		})
	}
	return list
}
