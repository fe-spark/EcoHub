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
	oldID, newID, name := parseHelperArgs(args)
	if oldID == "" || newID == "" {
		return fmt.Errorf("usage: upgrade-helper --old <id> --new <id> [--name <name>]")
	}
	engine, err := newDockerEngine()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// 确保向旧容器下发停止指令（即便主进程在退出时 stop 请求中断也能兜底触发）
	_ = engine.stop(ctx, oldID)

	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		running, err := engine.isRunning(ctx, oldID)
		if err != nil || !running {
			break
		}
		time.Sleep(time.Second)
	}
	if running, err := engine.isRunning(ctx, oldID); err == nil && running {
		log.Printf("[UpgradeHelper] 旧容器仍在运行，清理新容器并恢复旧容器名")
		_ = engine.remove(ctx, newID)
		if name != "" {
			_ = engine.rename(ctx, oldID, name)
		}
		return fmt.Errorf("旧容器仍在运行，放弃启动新容器以免端口冲突")
	}
	if err := engine.start(ctx, newID); err != nil {
		log.Printf("[UpgradeHelper] 启动新容器失败: %v，尝试回滚旧容器", err)
		_ = engine.remove(ctx, newID)
		if name != "" {
			_ = engine.rename(ctx, oldID, name)
		}
		if startErr := engine.start(ctx, oldID); startErr != nil {
			log.Printf("[UpgradeHelper] 回滚启动旧容器失败: %v", startErr)
		}
		return fmt.Errorf("启动新容器失败: %w", err)
	}
	if err := engine.remove(ctx, oldID); err != nil {
		log.Printf("[UpgradeHelper] 新容器已启动，删除旧容器失败: %v", err)
	}
	return nil
}

func parseHelperArgs(args []string) (oldID, newID, name string) {
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
		case "--name":
			if i+1 < len(args) {
				name = args[i+1]
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
	if name == "" {
		name = os.Getenv("ECOHUB_UPGRADE_NAME")
	}
	return oldID, newID, name
}
