package film

import (
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/snapshot"
)

// ResetProgress 数据重置实时进度（前端轮询展示真实进度）
type ResetProgress struct {
	Running bool   `json:"running"` // 重置是否仍在进行
	Percent int    `json:"percent"` // 0-100 真实完成百分比
	Stage   string `json:"stage"`   // 当前阶段描述
	Error   string `json:"error"`   // 失败原因（失败时非空）
}

// ResetImpactStats 工作台影视数据规模（按当前基准源隔离，不含其它采集站）
type ResetImpactStats struct {
	Films      int64  `json:"films"`
	Snapshots  int64  `json:"snapshots"`
	Categories int64  `json:"categories"`
	Failures   int64  `json:"failures"`
	SourceId   string `json:"sourceId,omitempty"`
	SourceName string `json:"sourceName,omitempty"`
}

// GetResetImpactStats 统计当前基准源的影片、分类与失败记录规模。
func GetResetImpactStats() ResetImpactStats {
	var stats ResetImpactStats
	if db.Mdb == nil {
		return stats
	}
	primary := repository.GetActiveCollectSource()
	if primary == nil || strings.TrimSpace(primary.Id) == "" {
		return stats
	}
	sourceID := primary.Id
	stats.SourceId = sourceID
	stats.SourceName = primary.Name

	_ = db.Mdb.Model(&model.FilmSourcePlaylist{}).
		Where("source_id = ? AND line_kind = ?", sourceID, "play").
		Select("COUNT(DISTINCT mid)").
		Scan(&stats.Films).Error

	version := snapshot.GetActiveSnapshotVersion()
	if version != "" {
		_ = db.Mdb.Model(&model.FilmSnapshotSource{}).
			Where("snapshot_version = ? AND source_id = ?", version, sourceID).
			Count(&stats.Snapshots).Error
	}

	stats.Categories = countCategoryTreeNodes(repository.GetActiveCategoryTree(sourceID).Children)

	_ = db.Mdb.Model(&model.FailureRecord{}).
		Where("origin_id = ?", sourceID).
		Count(&stats.Failures).Error
	return stats
}

func countCategoryTreeNodes(nodes []*model.CategoryTree) int64 {
	var n int64
	for _, node := range nodes {
		if node == nil {
			continue
		}
		n++
		n += countCategoryTreeNodes(node.Children)
	}
	return n
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
