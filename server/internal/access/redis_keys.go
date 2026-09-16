package access

import (
	"time"

	"server/internal/config"
)

const (
	ttlMinute = 48 * time.Hour
	ttlDay    = 14 * 24 * time.Hour
	slowKeep  = 200
	zsetKeep  = 5000
)

func minKey(t time.Time) string {
	return config.AccessKeyPrefix + "min:" + t.Format("200601021504")
}
func uvKey(day string) string     { return config.AccessKeyPrefix + "uv:" + day }
func dayAggKey(day string) string { return config.AccessKeyPrefix + "day:" + day }
func clientKey(day string) string { return config.AccessKeyPrefix + "client:" + day }
func actionKey(day string) string { return config.AccessKeyPrefix + "action:" + day }
func histKey(day string) string   { return config.AccessKeyPrefix + "hist:" + day }
func topPathKey(day string) string {
	return config.AccessKeyPrefix + "top:path:" + day
}
func topSearchKey(day string) string {
	return config.AccessKeyPrefix + "top:search:" + day
}
func topPlayKey(day string) string     { return config.AccessKeyPrefix + "top:play:" + day }
func topClassifyKey(day string) string { return config.AccessKeyPrefix + "top:classify:" + day }
func recentDayKey(day string) string   { return config.AccessKeyPrefix + "recent:" + day }
func droppedKey() string               { return config.AccessKeyPrefix + "meta:dropped" }
func droppedDayKey(day string) string  { return config.AccessKeyPrefix + "meta:dropped:" + day }

// Web 专属 Key
func webPVKey(day string) string          { return config.AccessKeyPrefix + "web:pv:" + day }
func webUVKey(day string) string          { return config.AccessKeyPrefix + "web:uv:" + day }
func webTopPageKey(day string) string     { return config.AccessKeyPrefix + "web:top:page:" + day }
func webTopPlayKey(day string) string     { return config.AccessKeyPrefix + "web:top:play:" + day }
func webTopSearchKey(day string) string   { return config.AccessKeyPrefix + "web:top:search:" + day }
func webTopClassifyKey(day string) string { return config.AccessKeyPrefix + "web:top:classify:" + day }
func webActionKey(day string) string      { return config.AccessKeyPrefix + "web:action:" + day }
func webRecentDayKey(day string) string   { return config.AccessKeyPrefix + "web:recent:" + day }
func webBrowsersKey(day string) string    { return config.AccessKeyPrefix + "web:browsers:" + day }
func webOSKey(day string) string          { return config.AccessKeyPrefix + "web:os:" + day }

// App 专属 Key
func appPVKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":pv:" + day
}
func appUVKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":uv:" + day
}
func appTopPageKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":top:page:" + day
}
func appTopPlayKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":top:play:" + day
}
func appTopSearchKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":top:search:" + day
}
func appTopClassifyKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":top:classify:" + day
}
func appVersionKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":versions:" + day
}
func appAllPVKey(day string) string      { return config.AccessKeyPrefix + "app:all:pv:" + day }
func appAllUVKey(day string) string      { return config.AccessKeyPrefix + "app:all:uv:" + day }
func appAllTopPageKey(day string) string { return config.AccessKeyPrefix + "app:all:top:page:" + day }
func appAllTopPlayKey(day string) string { return config.AccessKeyPrefix + "app:all:top:play:" + day }
func appAllTopSearchKey(day string) string {
	return config.AccessKeyPrefix + "app:all:top:search:" + day
}
func appAllTopClassifyKey(day string) string {
	return config.AccessKeyPrefix + "app:all:top:classify:" + day
}
func appActionKey(day string) string { return config.AccessKeyPrefix + "app:action:" + day }
func appPlatformActionKey(platform, day string) string {
	return config.AccessKeyPrefix + "app:" + platform + ":action:" + day
}
func appPlatformsKey(day string) string { return config.AccessKeyPrefix + "app:platforms:" + day }
func appModelsKey(day string) string    { return config.AccessKeyPrefix + "app:models:" + day }
func appRecentDayKey(day string) string { return config.AccessKeyPrefix + "app:recent:" + day }

// TVBox 专属 Key
func tvboxPVKey(day string) string        { return config.AccessKeyPrefix + "tvbox:pv:" + day }
func tvboxUVKey(day string) string        { return config.AccessKeyPrefix + "tvbox:uv:" + day }
func tvboxTopPlayKey(day string) string   { return config.AccessKeyPrefix + "tvbox:top:play:" + day }
func tvboxTopSearchKey(day string) string { return config.AccessKeyPrefix + "tvbox:top:search:" + day }
func tvboxTopClassifyKey(day string) string {
	return config.AccessKeyPrefix + "tvbox:top:classify:" + day
}
func tvboxActionKey(day string) string    { return config.AccessKeyPrefix + "tvbox:action:" + day }
func tvboxRecentDayKey(day string) string { return config.AccessKeyPrefix + "tvbox:recent:" + day }
