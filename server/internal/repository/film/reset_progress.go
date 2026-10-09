package film

import "sync"

// ResetProgress 数据重置实时进度（前端轮询展示真实进度）
type ResetProgress struct {
	Running bool   `json:"running"` // 重置是否仍在进行
	Percent int    `json:"percent"` // 0-100 真实完成百分比
	Stage   string `json:"stage"`   // 当前阶段描述
	Error   string `json:"error"`   // 失败原因（失败时非空）
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
