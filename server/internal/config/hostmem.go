package config

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// hostMemoryBytes 读取宿主物理内存。读不到时返回 0，选档只看 CPU。
func hostMemoryBytes() uint64 {
	switch runtime.GOOS {
	case "linux":
		text, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return 0
		}
		return parseMeminfoTotal(string(text))
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return 0
		}
		n, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

func parseMeminfoTotal(text string) uint64 {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}
