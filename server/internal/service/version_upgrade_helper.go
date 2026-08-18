package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"
)

// RunUpgradeHelper 在独立容器中运行：等旧容器退出后 start 新容器并删除旧容器。
func RunUpgradeHelper(args []string) error {
	oldID, newID := parseHelperArgs(args)
	if oldID == "" || newID == "" {
		return fmt.Errorf("usage: upgrade-helper --old <id> --new <id>")
	}
	engine, err := newDockerEngine()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		running, err := engine.isRunning(ctx, oldID)
		if err != nil || !running {
			break
		}
		time.Sleep(time.Second)
	}
	if running, err := engine.isRunning(ctx, oldID); err == nil && running {
		return fmt.Errorf("旧容器仍在运行，放弃启动新容器以免端口冲突")
	}
	if err := engine.start(ctx, newID); err != nil {
		return fmt.Errorf("启动新容器失败: %w", err)
	}
	if err := engine.remove(ctx, oldID); err != nil {
		log.Printf("[UpgradeHelper] 新容器已启动，删除旧容器失败: %v", err)
	}
	return nil
}

func parseHelperArgs(args []string) (oldID, newID string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--old":
			if i+1 < len(args) {
				oldID = args[i+1]
				i++
			}
		case "--new":
			if i+1 < len(args) {
				newID = args[i+1]
				i++
			}
		}
	}
	if oldID == "" {
		oldID = os.Getenv("ECOHUB_UPGRADE_OLD")
	}
	if newID == "" {
		newID = os.Getenv("ECOHUB_UPGRADE_NEW")
	}
	return oldID, newID
}
