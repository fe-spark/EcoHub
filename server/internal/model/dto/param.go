package dto

import (
	"reflect"
)

// IsEmpty 判断数据值是否等于默认零值
func IsEmpty(v any) bool {
	return v == nil || reflect.DeepEqual(v, reflect.Zero(reflect.TypeOf(v)).Interface())
}
