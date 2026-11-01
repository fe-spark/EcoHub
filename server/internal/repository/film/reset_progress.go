package film

import "sync"

// ResetProgress 数据重置实时进度，由 SSE 推给页面。
type ResetProgress struct {
	Running bool   `json:"running"` // 重置是否仍在进行
	Percent int    `json:"percent"` // 0-100 真实完成百分比
	Stage   string `json:"stage"`   // 当前阶段描述
	Error   string `json:"error"`   // 失败原因（失败时非空）
}

var resetProg = struct {
	mu       sync.Mutex
	running  bool
	percent  int
	stage    string
	errMsg   string
	nextID   int
	watchers map[int]func(ResetProgress)
}{watchers: map[int]func(ResetProgress){}}

// WatchResetProgress 订阅后续进度。取消订阅后不再收到事件。
func WatchResetProgress(fn func(ResetProgress)) func() {
	if fn == nil {
		return func() {}
	}
	resetProg.mu.Lock()
	resetProg.nextID++
	id := resetProg.nextID
	resetProg.watchers[id] = fn
	resetProg.mu.Unlock()
	return func() {
		resetProg.mu.Lock()
		delete(resetProg.watchers, id)
		resetProg.mu.Unlock()
	}
}

func publishResetProgress() {
	resetProg.mu.Lock()
	snap := ResetProgress{
		Running: resetProg.running,
		Percent: resetProg.percent,
		Stage:   resetProg.stage,
		Error:   resetProg.errMsg,
	}
	watchers := make([]func(ResetProgress), 0, len(resetProg.watchers))
	for _, fn := range resetProg.watchers {
		watchers = append(watchers, fn)
	}
	resetProg.mu.Unlock()
	for _, fn := range watchers {
		fn(snap)
	}
}

// StartResetProgress 标记一次重置开始
func StartResetProgress() {
	resetProg.mu.Lock()
	resetProg.running = true
	resetProg.percent = 3
	resetProg.stage = "正在启动重置"
	resetProg.errMsg = ""
	resetProg.mu.Unlock()
	publishResetProgress()
}

// ReportResetProgress 更新重置进度
func ReportResetProgress(percent int, stage string) {
	resetProg.mu.Lock()
	resetProg.percent = percent
	if stage != "" {
		resetProg.stage = stage
	}
	resetProg.mu.Unlock()
	publishResetProgress()
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
	publishResetProgress()
}

// GetResetProgress 获取当前重置进度
func GetResetProgress() ResetProgress {
	resetProg.mu.Lock()
	defer resetProg.mu.Unlock()
	return ResetProgress{
		Running: resetProg.running,
		Percent: resetProg.percent,
		Stage:   resetProg.stage,
		Error:   resetProg.errMsg,
	}
}
