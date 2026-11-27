package db

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// IsDuplicateKeyError 判断是否为数据库唯一键冲突（MySQL 1062 / SQLite UNIQUE）。
func IsDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") ||
		strings.Contains(msg, "Error 1062") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}
