package backup

import (
	"server/internal/model"
	"testing"
)

func TestValidateImportPayload_RejectsInvalidMappingAndNotify(t *testing.T) {
	// 1. 非法正则映射规则
	err := validateImportPayload(model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{MappingRules: true},
		Backup: model.ConfigBackup{
			MappingRules: []model.MappingRuleExport{
				{SourceId: "src1", Group: "CategoryRoot", Raw: "[unclosed", Target: "电影", MatchType: "regex"},
			},
		},
	})
	if err == nil {
		t.Fatal("非法正则表达式映射规则应被前置校验拒绝")
	}

	// 2. 规范化后重复生效点的映射规则
	err = validateImportPayload(model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{MappingRules: true},
		Backup: model.ConfigBackup{
			MappingRules: []model.MappingRuleExport{
				{Group: "Area", Raw: "中国大陆", Target: "内地", MatchType: "exact"},
				{Group: "Area", Raw: " 中国大陆 ", Target: "大陆", MatchType: ""},
			},
		},
	})
	if err == nil {
		t.Fatal("规范化后重复的映射规则应被前置校验拒绝")
	}

	// 3. 启用通知但缺少 BotToken
	err = validateImportPayload(model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{Notify: true},
		Backup: model.ConfigBackup{
			Notify: &model.NotifyConfig{
				Enabled: true,
				ChatIDs: []string{"-100123456"},
			},
		},
	})
	if err == nil {
		t.Fatal("启用通知但缺少 BotToken 应被前置校验拒绝")
	}

	// 4. 非法 BotToken 与非法话题 ChatID
	err = validateImportPayload(model.ConfigBackupImportRequest{
		Modules: model.ConfigBackupModules{Notify: true},
		Backup: model.ConfigBackup{
			Notify: &model.NotifyConfig{
				Enabled:  false,
				BotToken: "123:bad/token",
				ChatIDs:  []string{"-100123456:notanumber"},
			},
		},
	})
	if err == nil {
		t.Fatal("非法 BotToken / ChatID 应被前置校验拒绝")
	}
}
