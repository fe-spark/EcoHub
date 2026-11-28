package analytics

import (
	"time"

	"server/internal/analytics/config"
	"server/internal/analytics/handler"
	"server/internal/analytics/ingest"
	"server/internal/analytics/query"
	"server/internal/analytics/rollup"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	"server/internal/model"

	"github.com/gin-gonic/gin"
)

// Handler HTTP 处理器实例
var Handler = handler.Handler

// Start 启动数据分析异步采集队列与日终归档调度
func Start() {
	rollup.StartScheduler()
	ingest.StartCollector()
}

// Shutdown 优雅停止采集队列与归档调度，并在超时窗口内排空缓冲事件
func Shutdown(timeout time.Duration) {
	ingest.Shutdown(timeout)
	rollup.StopScheduler()
}

// IsEnabled 读取数据分析总开关内存状态
func IsEnabled() bool {
	return config.IsEnabled()
}

// SetEnabled 热更新数据分析总开关内存状态
func SetEnabled(val bool) {
	config.SetEnabled(val)
}

// GetConfig 读取数据分析配置
func GetConfig() (model.AnalyticsConfig, error) {
	return config.GetConfig()
}

// SaveConfig 保存数据分析配置
func SaveConfig(cfg model.AnalyticsConfig) error {
	return config.SaveConfig(cfg)
}

// ClearData 清理数据分析积累的历史统计与缓存
func ClearData(retentionDays int) (types.ClearResult, error) {
	return store.ClearData(retentionDays, rollup.Mutex(), query.ClearOverviewCache)
}

// FromContext 从 HTTP 请求构建埋点事件
func FromContext(c *gin.Context, elapsed time.Duration) *types.Event {
	return ingest.FromContext(c, elapsed)
}

// Collect 非阻塞投递分析事件至异步队列
func Collect(evt *types.Event) {
	ingest.Collect(evt)
}

// ShouldSkip 判断基础噪声请求是否跳过访问日志输出
func ShouldSkip(method, path string, status int) bool {
	return ingest.ShouldSkip(method, path, status)
}

// IPPreview 生成脱敏展示 IP
func IPPreview(ip string) string {
	return ingest.IPPreview(ip)
}
