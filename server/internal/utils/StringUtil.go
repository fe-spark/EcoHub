package utils

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
)

var seriesSuffixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`[\s·\-_]*第?\s*[一二三四五六七八九十百千万零两0-9]+\s*(季|部|篇|章)$`),
	regexp.MustCompile(`(?i)[\s·\-_]*(season\s*[0-9]+|s[0-9]{1,2})$`),
	regexp.MustCompile(`(?:\(|（|\[|【)\s*第?\s*[一二三四五六七八九十百千万零两0-9]+\s*(季|部|篇|章)\s*(?:\)|）|\]|】)$`),
}

// GenerateUUID 生成UUID
func GenerateUUID() (uuid string) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatal(err)
	}
	uuid = fmt.Sprintf("%X-%X-%X-%X-%X",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return
}

// RandomString 生成指定长度两倍的随机字符串
func RandomString(length int) (uuid string) {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatal(err)
	}
	uuid = fmt.Sprintf("%x", b)
	return
}

// GenerateSalt 生成 length为16的随机字符串
func GenerateSalt() (uuid string) {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatal(err)
	}
	uuid = fmt.Sprintf("%X", b)
	return
}

// PasswordEncrypt 密码加密 , (password+salt) md5 * 3
func PasswordEncrypt(password, salt string) string {
	b := fmt.Append(nil, password, salt) // 将字符串转换为字节切片
	var r [16]byte
	for range 3 {
		r = md5.Sum(b) // 调用md5.Sum()函数进行加密
		b = []byte(hex.EncodeToString(r[:]))
	}
	return hex.EncodeToString(r[:])
}

// ValidURL 校验http链接是否是符合规范的URL
func ValidURL(s string) bool {
	_, err := url.ParseRequestURI(s)
	return err == nil
}

func ValidPwd(s string) error {
	if len(s) < 8 || len(s) > 12 {
		return fmt.Errorf("密码长度不符合规范, 必须为8-10位")
	}
	// 分别校验数字 大小写字母和特殊字符
	num := `[0-9]{1}`
	l := `[a-z]{1}`
	u := `[A-Z]{1}`
	symbol := `[!@#~$%^&*()+|_]{1}`
	if b, err := regexp.MatchString(num, s); !b || err != nil {
		return errors.New("密码必须包含数字 ")
	}
	if b, err := regexp.MatchString(l, s); !b || err != nil {
		return errors.New("密码必须包含小写字母")
	}
	if b, err := regexp.MatchString(u, s); !b || err != nil {
		return errors.New("密码必须包含大写字母")
	}
	if b, err := regexp.MatchString(symbol, s); !b || err != nil {
		return errors.New("密码必须包含特殊字")
	}
	return nil
}

// ContainsAny 判断字符串是否包含切片中的任意一个关键词
func ContainsAny(s string, keywords []string) bool {
	if keywords == nil {
		return false
	}
	for _, kw := range keywords {
		if kw != "" && (s == kw || (len(kw) > 0 && (strings.Contains(s, kw)))) {
			return true
		}
	}
	return false
}

func NormalizeTitleCandidates(title string) []string {
	base := strings.TrimSpace(title)
	if base == "" {
		return nil
	}

	candidates := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	appendCandidate := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		candidates = append(candidates, v)
	}

	appendCandidate(base)
	compact := regexp.MustCompile(`\s+`).ReplaceAllString(base, "")
	appendCandidate(compact)

	for _, p := range seriesSuffixPatterns {
		trimmed := strings.TrimSpace(p.ReplaceAllString(base, ""))
		appendCandidate(trimmed)
		if trimmed != "" {
			appendCandidate(regexp.MustCompile(`\s+`).ReplaceAllString(trimmed, ""))
		}
	}

	return candidates
}
