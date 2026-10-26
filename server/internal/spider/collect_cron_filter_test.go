package spider

import (
	"testing"

	"server/internal/model"
)

func TestAutoCollectTriggered_NoSources(t *testing.T) {
	// DB 为空或无启用站点时应平稳跳过，不 panic
	AutoCollectTriggered(model.CollectTriggerCron, 24)
	AutoCollect(24)
}
