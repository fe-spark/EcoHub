package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// flexString 接受 JSON 字符串或数字，统一存成字符串。
// 采集站常把 vod_serial 等字段输出成数字，标准 encoding/json 不能直接写入 string。
type flexString string

func (s *flexString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}
	if data[0] == '"' {
		var v string
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*s = flexString(v)
		return nil
	}
	if data[0] == '-' || (data[0] >= '0' && data[0] <= '9') {
		var n json.Number
		if err := json.Unmarshal(data, &n); err != nil {
			return err
		}
		*s = flexString(n.String())
		return nil
	}
	return fmt.Errorf("字段需为字符串或数字")
}

// flexInt64 接受 JSON 数字或数字字符串，统一存成 int64；null 与空字符串记为 0。
// 部分采集站会把 vod_score_num 等整型字段输出成 "10" 这样的字符串，
// 标准 encoding/json 直接报错，导致整页失败。小数按截断取整，越界或非数字文本报错。
type flexInt64 int64

func (n *flexInt64) UnmarshalJSON(data []byte) error {
	v, err := parseFlexInt(data, false)
	*n = flexInt64(v)
	return err
}

// flexID 同 flexInt64，但只收整数值。ID 带小数说明数据有问题，截断会悄悄指错片或分类。
type flexID int64

func (n *flexID) UnmarshalJSON(data []byte) error {
	v, err := parseFlexInt(data, true)
	*n = flexID(v)
	return err
}

func parseFlexInt(data []byte, integral bool) (int64, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return 0, nil
	}
	text := string(data)
	if data[0] == '"' {
		if err := json.Unmarshal(data, &text); err != nil {
			return 0, err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return 0, nil
		}
	}
	if v, err := strconv.ParseInt(text, 10, 64); err == nil {
		return v, nil
	}
	f, err := strconv.ParseFloat(text, 64)
	// float64(MaxInt64) 等于 2^63，已经越界，所以上界用 >=。
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f >= math.MaxInt64 || f < math.MinInt64 {
		return 0, fmt.Errorf("字段需为整数或数字字符串: %s", text)
	}
	if integral && f != math.Trunc(f) {
		return 0, fmt.Errorf("字段需为整数: %s", text)
	}
	return int64(f), nil
}

// UnmarshalJSON 兼容采集详情里站点常输出成「另一种类型」的字段：
// 声明为字符串的字段接受数字，声明为整型的字段接受数字字符串。
// 片名、简介、播放地址等其余文本字段保持原类型。
func (f *FilmDetail) UnmarshalJSON(data []byte) error {
	type alias FilmDetail
	aux := struct {
		VodSerial      flexString `json:"vod_serial"`
		VodYear        flexString `json:"vod_year"`
		VodDuration    flexString `json:"vod_duration"`
		VodScore       flexString `json:"vod_score"`
		VodDouBanScore flexString `json:"vod_douban_score"`
		VodID          flexID     `json:"vod_id"`
		TypeID         flexID     `json:"type_id"`
		TypeID1        flexID     `json:"type_id_1"`
		GroupID        flexID     `json:"group_id"`
		VodStatus      flexInt64  `json:"vod_status"`
		VodTotal       flexInt64  `json:"vod_total"`
		VodIsEnd       flexInt64  `json:"vod_isend"`
		VodLock        flexInt64  `json:"vod_lock"`
		VodLevel       flexInt64  `json:"vod_level"`
		VodCopyright   flexInt64  `json:"vod_copyright"`
		VodPoints      flexInt64  `json:"vod_points"`
		VodPointsPlay  flexInt64  `json:"vod_points_play"`
		VodPointsDown  flexInt64  `json:"vod_points_down"`
		VodHits        flexInt64  `json:"vod_hits"`
		VodHitsDay     flexInt64  `json:"vod_hits_day"`
		VodHitsWeek    flexInt64  `json:"vod_hits_week"`
		VodHitsMonth   flexInt64  `json:"vod_hits_month"`
		VodUp          flexInt64  `json:"vod_up"`
		VodDown        flexInt64  `json:"vod_down"`
		VodScoreAll    flexInt64  `json:"vod_score_all"`
		VodScoreNum    flexInt64  `json:"vod_score_num"`
		VodTimeAdd     flexInt64  `json:"vod_time_add"`
		VodTimeHits    flexInt64  `json:"vod_time_hits"`
		VodTimeMake    flexInt64  `json:"vod_time_make"`
		VodTrySee      flexInt64  `json:"vod_trysee"`
		VodDouBanID    flexInt64  `json:"vod_douban_id"`
		VodPlot        flexInt64  `json:"vod_plot"`
		*alias
	}{alias: (*alias)(f)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	f.VodSerial = string(aux.VodSerial)
	f.VodYear = string(aux.VodYear)
	f.VodDuration = string(aux.VodDuration)
	f.VodScore = string(aux.VodScore)
	f.VodDouBanScore = string(aux.VodDouBanScore)
	f.VodID = int64(aux.VodID)
	f.TypeID = int64(aux.TypeID)
	f.TypeID1 = int64(aux.TypeID1)
	f.GroupID = int(aux.GroupID)
	f.VodStatus = int64(aux.VodStatus)
	f.VodTotal = int64(aux.VodTotal)
	f.VodIsEnd = int64(aux.VodIsEnd)
	f.VodLock = int64(aux.VodLock)
	f.VodLevel = int64(aux.VodLevel)
	f.VodCopyright = int64(aux.VodCopyright)
	f.VodPoints = int64(aux.VodPoints)
	f.VodPointsPlay = int64(aux.VodPointsPlay)
	f.VodPointsDown = int64(aux.VodPointsDown)
	f.VodHits = int64(aux.VodHits)
	f.VodHitsDay = int64(aux.VodHitsDay)
	f.VodHitsWeek = int64(aux.VodHitsWeek)
	f.VodHitsMonth = int64(aux.VodHitsMonth)
	f.VodUp = int64(aux.VodUp)
	f.VodDown = int64(aux.VodDown)
	f.VodScoreAll = int64(aux.VodScoreAll)
	f.VodScoreNum = int64(aux.VodScoreNum)
	f.VodTimeAdd = int64(aux.VodTimeAdd)
	f.VodTimeHits = int64(aux.VodTimeHits)
	f.VodTimeMake = int64(aux.VodTimeMake)
	f.VodTrySee = int64(aux.VodTrySee)
	f.VodDouBanID = int64(aux.VodDouBanID)
	f.VodPlot = int64(aux.VodPlot)
	return nil
}

// UnmarshalJSON 列表接口（ac=list）的 vod_id、type_id 同样兼容数字字符串。
func (f *FilmList) UnmarshalJSON(data []byte) error {
	type alias FilmList
	aux := struct {
		VodID  flexID `json:"vod_id"`
		TypeID flexID `json:"type_id"`
		*alias
	}{alias: (*alias)(f)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	f.VodID = int64(aux.VodID)
	f.TypeID = int64(aux.TypeID)
	return nil
}

// UnmarshalJSON 分类列表的 type_id、type_pid 同样兼容数字字符串。
func (c *FilmClass) UnmarshalJSON(data []byte) error {
	type alias FilmClass
	aux := struct {
		ID  flexID `json:"type_id"`
		Pid flexID `json:"type_pid"`
		*alias
	}{alias: (*alias)(c)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	c.ID = int64(aux.ID)
	c.Pid = int64(aux.Pid)
	return nil
}

// UnmarshalJSON 探页用的分页外壳：pagecount、total 有的站输出成字符串。
func (p *CommonPage) UnmarshalJSON(data []byte) error {
	type alias CommonPage
	aux := struct {
		PageCount flexInt64 `json:"pagecount"`
		Total     flexInt64 `json:"total"`
		*alias
	}{alias: (*alias)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.PageCount, p.Total = int(aux.PageCount), int(aux.Total)
	return nil
}

// UnmarshalJSON 列表分页外壳，同 CommonPage。
func (p *FilmListPage) UnmarshalJSON(data []byte) error {
	type alias FilmListPage
	aux := struct {
		PageCount flexInt64 `json:"pagecount"`
		Total     flexInt64 `json:"total"`
		*alias
	}{alias: (*alias)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.PageCount, p.Total = int(aux.PageCount), int(aux.Total)
	return nil
}

// UnmarshalJSON 详情分页外壳，同 CommonPage。
func (p *FilmDetailLPage) UnmarshalJSON(data []byte) error {
	type alias FilmDetailLPage
	aux := struct {
		PageCount flexInt64 `json:"pagecount"`
		Total     flexInt64 `json:"total"`
		*alias
	}{alias: (*alias)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.PageCount, p.Total = int(aux.PageCount), int(aux.Total)
	return nil
}
