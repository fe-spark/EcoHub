package model

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// UnmarshalJSON 只兼容采集详情里声明为字符串、站点常输出成数字的字段。
// 片名、简介、播放地址等文本，以及 vod_id、vod_hits 等整型，保持原类型。
func (f *FilmDetail) UnmarshalJSON(data []byte) error {
	type alias FilmDetail
	aux := struct {
		VodSerial      flexString `json:"vod_serial"`
		VodYear        flexString `json:"vod_year"`
		VodDuration    flexString `json:"vod_duration"`
		VodScore       flexString `json:"vod_score"`
		VodDouBanScore flexString `json:"vod_douban_score"`
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
	return nil
}
