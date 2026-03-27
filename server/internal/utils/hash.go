package utils

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

// GenerateHashKey 存储播放源信息时对影片名称进行处理, 提高各站点间同一影片的匹配度
func GenerateHashKey[K string | ~int | int64](key K) string {
	mName := fmt.Sprint(key)

	// 1. 繁体转简体
	mName = TraditionalToSimplified(mName)

	// 2. 转小写
	mName = strings.ToLower(mName)

	// 3. 去除所有非中英数字符（保留中文字符、英文字母、数字）
	// \p{L} 包含所有字母，\p{N} 包含所有数字, \p{Han} 包含中文
	reg := regexp.MustCompile(`[^\p{L}\p{N}\p{Han}]`)
	mName = reg.ReplaceAllString(mName, "")

	// 4. 特殊后缀归一化（如：第一季 -> 1季，剧场版 -> 剧场）
	mName = regexp.MustCompile(`第([一二三四五六七八九十]|[\d]+)季`).ReplaceAllString(mName, "季")
	mName = regexp.MustCompile(`第([一二三四五六七八九十]|[\d]+)部`).ReplaceAllString(mName, "部")
	mName = strings.ReplaceAll(mName, "剧场版", "剧场")

	// 5. 将处理完成后的name转化为hash值作为存储时的key
	h := fnv.New32a()
	_, err := h.Write([]byte(mName))
	if err != nil {
		return ""
	}
	return fmt.Sprint(h.Sum32())
}
