package config

import (
	"os"
	"testing"
)

func TestIsUpgradeHelper(t *testing.T) {
	// 默认非 helper 状态
	t.Setenv("ECOHUB_UPGRADE_HELPER", "")
	if IsUpgradeHelper() {
		t.Fatal("expected IsUpgradeHelper to be false by default")
	}

	// 环境变量生效
	t.Setenv("ECOHUB_UPGRADE_HELPER", "1")
	if !IsUpgradeHelper() {
		t.Fatal("expected IsUpgradeHelper to be true when ECOHUB_UPGRADE_HELPER=1")
	}

	// 命令行参数生效
	t.Setenv("ECOHUB_UPGRADE_HELPER", "")
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"/app/server/main", "upgrade-helper", "--old", "aaa", "--new", "bbb"}
	if !IsUpgradeHelper() {
		t.Fatal("expected IsUpgradeHelper to be true when os.Args contains upgrade-helper")
	}
}
