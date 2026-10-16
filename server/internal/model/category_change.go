package model

const (
	CategoryChangeRenamed = "renamed"
	CategoryChangeRemoved = "removed"
	CategoryChangeMoved   = "moved"

	CategorySyncKeep  = "keep"
	CategorySyncClear = "clear"
)

// CategoryTreeChange 表示单站分类树同步中的差异项
type CategoryTreeChange struct {
	TypeId int64  `json:"typeId"`
	Kind   string `json:"kind"` // renamed | removed | moved
	Name   string `json:"name,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

// CategorySyncResult 分类同步结果
type CategorySyncResult struct {
	NeedConfirm bool                 `json:"needConfirm"`
	Id          string               `json:"id"`
	Name        string               `json:"name"`
	Changes     []CategoryTreeChange `json:"changes"`
	DataCleared bool                 `json:"dataCleared,omitempty"`
	Message     string               `json:"message,omitempty"`
}
