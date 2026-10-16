package handler

import (
	"fmt"
	"strings"

	"server/internal/model"
)

func batchOutcomeMessage(result model.CollectBatchResult, startedVerb, emptyLabel string) string {
	if len(result.Started) == 0 {
		if len(result.Skipped) == 0 {
			return emptyLabel
		}
		parts := make([]string, 0, len(result.Skipped))
		for _, item := range result.Skipped {
			name := item.Name
			if name == "" {
				name = item.Id
			}
			parts = append(parts, name+"（"+item.Reason+"）")
		}
		return emptyLabel + "：" + strings.Join(parts, "；")
	}
	msg := fmt.Sprintf("%s %d 个", startedVerb, len(result.Started))
	if len(result.Skipped) > 0 {
		msg += fmt.Sprintf("，跳过 %d 个", len(result.Skipped))
	}
	return msg
}
