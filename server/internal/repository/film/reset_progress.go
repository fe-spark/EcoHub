package film

import (
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
)

// ResetProgress 数据重置实时进度（前端轮询展示真实进度）
type ResetProgress struct {
	Running bool   `json:"running"` // 重置是否仍在进行
	Percent int    `json:"percent"` // 0-100 真实完成百分比
	Stage   string `json:"stage"`   // 当前阶段描述
	Error   string `json:"error"`   // 失败原因（失败时非空）
}

// ResetImpactStats 数据重置影响面统计（将清空的数据量）
type ResetImpactStats struct {
	Films      int64 `json:"films"`      // 影视库存
	Snapshots  int64 `json:"snapshots"`  // 列表快照
	Categories int64 `json:"categories"` // 分类
	Failures   int64 `json:"failures"`   // 失败记录
}

// GetResetImpactStats 统计将被数据重置清空的数据量，用于重置前展示影响面
func GetResetImpactStats() ResetImpactStats {
	var stats ResetImpactStats
	db.Mdb.Model(&model.FilmIndex{}).Count(&stats.Films)
	db.Mdb.Model(&model.FilmListSnapshot{}).Count(&stats.Snapshots)
	db.Mdb.Model(&model.Category{}).Count(&stats.Categories)
	db.Mdb.Model(&model.FailureRecord{}).Count(&stats.Failures)
	return stats
}

var resetProg = struct {
	mu      sync.RWMutex
	running bool
	percent int
	stage   string
	errMsg  string
}{}

// StartResetProgress 标记一次重置开始
func StartResetProgress() {
	resetProg.mu.Lock()
	resetProg.running = true
	resetProg.percent = 3
	resetProg.stage = "正在启动重置"
	resetProg.errMsg = ""
	resetProg.mu.Unlock()
}

// ReportResetProgress 更新重置进度
func ReportResetProgress(percent int, stage string) {
	resetProg.mu.Lock()
	resetProg.percent = percent
	if stage != "" {
		resetProg.stage = stage
	}
	resetProg.mu.Unlock()
}

// FinishResetProgress 结束重置：成功置 100%，失败记录错误信息
func FinishResetProgress(err error) {
	resetProg.mu.Lock()
	resetProg.running = false
	if err != nil {
		resetProg.errMsg = err.Error()
	} else {
		resetProg.percent = 100
		resetProg.stage = "重置完成"
		resetProg.errMsg = ""
	}
	resetProg.mu.Unlock()
}

// GetResetProgress 获取当前重置进度
func GetResetProgress() ResetProgress {
	resetProg.mu.RLock()
	defer resetProg.mu.RUnlock()
	return ResetProgress{
		Running: resetProg.running,
		Percent: resetProg.percent,
		Stage:   resetProg.stage,
		Error:   resetProg.errMsg,
	}
}
