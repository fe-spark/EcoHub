package store

import (
	"fmt"
	"time"

	appcfg "server/internal/config"
)

const (
	TTLDay           = 14 * 24 * time.Hour
	SlotsPerDay      = 96
	SlotMinutes      = 15
	RecentKeepLimit  = 100
	ZSetKeepLimit    = 5000
	RolledDayMetaKey = appcfg.AnalyticsKeyPrefix + "meta:rolled_day"
)

// BucketKey 单日 15 分钟时间槽聚合 Hash（1 天仅 1 个 Key 覆盖全部 96 个时间段）
func BucketKey(day string) string { return appcfg.AnalyticsKeyPrefix + "bucket:" + day }

// BucketSlotIndex 计算本地时间对应的 15 分钟槽位索引 (0..95)
func BucketSlotIndex(t time.Time) int {
	local := t.In(time.Local)
	idx := local.Hour()*4 + local.Minute()/SlotMinutes
	if idx < 0 {
		return 0
	}
	if idx >= SlotsPerDay {
		return SlotsPerDay - 1
	}
	return idx
}

// BucketField 构造时间槽 Hash 字段名
func BucketField(slot int, metric string) string {
	return fmt.Sprintf("%d:%s", slot, metric)
}

func UVKey(day string) string            { return appcfg.AnalyticsKeyPrefix + "uv:" + day }
func DayAggKey(day string) string        { return appcfg.AnalyticsKeyPrefix + "day:" + day }
func ClientKey(day string) string        { return appcfg.AnalyticsKeyPrefix + "client:" + day }
func ActionKey(day string) string        { return appcfg.AnalyticsKeyPrefix + "action:" + day }
func HistKey(day string) string          { return appcfg.AnalyticsKeyPrefix + "hist:" + day }
func TopPathKey(day string) string       { return appcfg.AnalyticsKeyPrefix + "top:path:" + day }
func TopSearchKey(day string) string     { return appcfg.AnalyticsKeyPrefix + "top:search:" + day }
func TopPlayKey(day string) string       { return appcfg.AnalyticsKeyPrefix + "top:play:" + day }
func TopClassifyKey(day string) string   { return appcfg.AnalyticsKeyPrefix + "top:classify:" + day }
func DroppedKey() string                 { return appcfg.AnalyticsKeyPrefix + "meta:dropped" }
func DroppedDayKey(day string) string    { return appcfg.AnalyticsKeyPrefix + "meta:dropped:" + day }
func CollectSourceKey(day string) string { return appcfg.AnalyticsKeyPrefix + "collect_source:" + day }

// Web 专属 Key
func WebPVKey(day string) string          { return appcfg.AnalyticsKeyPrefix + "web:pv:" + day }
func WebUVKey(day string) string          { return appcfg.AnalyticsKeyPrefix + "web:uv:" + day }
func WebTopPageKey(day string) string     { return appcfg.AnalyticsKeyPrefix + "web:top:page:" + day }
func WebTopPlayKey(day string) string     { return appcfg.AnalyticsKeyPrefix + "web:top:play:" + day }
func WebTopSearchKey(day string) string   { return appcfg.AnalyticsKeyPrefix + "web:top:search:" + day }
func WebTopClassifyKey(day string) string { return appcfg.AnalyticsKeyPrefix + "web:top:classify:" + day }
func WebActionKey(day string) string      { return appcfg.AnalyticsKeyPrefix + "web:action:" + day }
func WebRecentDayKey(day string) string   { return appcfg.AnalyticsKeyPrefix + "web:recent:" + day }
func WebBrowsersKey(day string) string    { return appcfg.AnalyticsKeyPrefix + "web:browsers:" + day }
func WebOSKey(day string) string          { return appcfg.AnalyticsKeyPrefix + "web:os:" + day }

// App 专属 Key
func AppPVKey(platform, day string) string {
	return appcfg.AnalyticsKeyPrefix + "app:" + platform + ":pv:" + day
}
func AppUVKey(platform, day string) string {
	return appcfg.AnalyticsKeyPrefix + "app:" + platform + ":uv:" + day
}
func AppTopPageKey(platform, day string) string {
	return appcfg.AnalyticsKeyPrefix + "app:" + platform + ":top:page:" + day
}
func AppTopPlayKey(platform, day string) string {
	return appcfg.AnalyticsKeyPrefix + "app:" + platform + ":top:play:" + day
}
func AppTopSearchKey(platform, day string) string {
	return appcfg.AnalyticsKeyPrefix + "app:" + platform + ":top:search:" + day
}
func AppTopClassifyKey(platform, day string) string {
	return appcfg.AnalyticsKeyPrefix + "app:" + platform + ":top:classify:" + day
}
func AppVersionKey(platform, day string) string {
	return appcfg.AnalyticsKeyPrefix + "app:" + platform + ":versions:" + day
}
func AppPlatformActionKey(platform, day string) string {
	return appcfg.AnalyticsKeyPrefix + "app:" + platform + ":action:" + day
}
func AppAllPVKey(day string) string      { return appcfg.AnalyticsKeyPrefix + "app:all:pv:" + day }
func AppAllUVKey(day string) string      { return appcfg.AnalyticsKeyPrefix + "app:all:uv:" + day }
func AppAllTopPageKey(day string) string { return appcfg.AnalyticsKeyPrefix + "app:all:top:page:" + day }
func AppAllTopPlayKey(day string) string { return appcfg.AnalyticsKeyPrefix + "app:all:top:play:" + day }
func AppAllTopSearchKey(day string) string {
	return appcfg.AnalyticsKeyPrefix + "app:all:top:search:" + day
}
func AppAllTopClassifyKey(day string) string {
	return appcfg.AnalyticsKeyPrefix + "app:all:top:classify:" + day
}
func AppActionKey(day string) string    { return appcfg.AnalyticsKeyPrefix + "app:action:" + day }
func AppPlatformsKey(day string) string { return appcfg.AnalyticsKeyPrefix + "app:platforms:" + day }
func AppModelsKey(day string) string    { return appcfg.AnalyticsKeyPrefix + "app:models:" + day }
func AppRecentDayKey(day string) string { return appcfg.AnalyticsKeyPrefix + "app:recent:" + day }

// TVBox 专属 Key
func TvboxPVKey(day string) string        { return appcfg.AnalyticsKeyPrefix + "tvbox:pv:" + day }
func TvboxUVKey(day string) string        { return appcfg.AnalyticsKeyPrefix + "tvbox:uv:" + day }
func TvboxTopPlayKey(day string) string   { return appcfg.AnalyticsKeyPrefix + "tvbox:top:play:" + day }
func TvboxTopSearchKey(day string) string { return appcfg.AnalyticsKeyPrefix + "tvbox:top:search:" + day }
func TvboxTopClassifyKey(day string) string {
	return appcfg.AnalyticsKeyPrefix + "tvbox:top:classify:" + day
}
func TvboxActionKey(day string) string    { return appcfg.AnalyticsKeyPrefix + "tvbox:action:" + day }
func TvboxRecentDayKey(day string) string { return appcfg.AnalyticsKeyPrefix + "tvbox:recent:" + day }
