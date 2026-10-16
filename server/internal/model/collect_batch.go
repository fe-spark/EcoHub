package model

// CollectBatchItem 批量动作里实际开始的采集站。
type CollectBatchItem struct {
	Id   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// CollectBatchSkip 批量动作里被跳过的采集站及原因。
type CollectBatchSkip struct {
	Id     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
}

// CollectBatchResult 批量动作的开始与跳过名单。
type CollectBatchResult struct {
	Started []CollectBatchItem `json:"started"`
	Skipped []CollectBatchSkip `json:"skipped"`
}
