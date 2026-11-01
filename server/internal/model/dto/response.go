package dto

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

/*
	对 http response 做简单的封装
*/

const (
	SUCCESS = 0
	FAILED  = -1
)

// Response http返回数据结构体
type Response struct {
	Code int    `json:"code"` // 状态 ok | failed
	Data any    `json:"data"` // 数据
	Msg  string `json:"msg"`  // 提示信息
}

// Page 分页信息结构体
type Page struct {
	PageSize  int `json:"pageSize"`  // 每页大小
	Current   int `json:"current"`   // 当前页
	PageCount int `json:"pageCount"` // 总页数
	Total     int `json:"total"`     // 总记录数
}

// Result 构建response返回数据结构
func Result(code int, data any, msg string, c *gin.Context) {
	c.JSON(http.StatusOK, Response{
		Code: code,
		Data: data,
		Msg:  msg,
	})
}

// Success 成功响应 数据 + 成功提示
func Success(data any, message string, c *gin.Context) {
	Result(SUCCESS, data, message, c)
}

// SuccessOnlyMsg 成功响应, 只返回成功信息
func SuccessOnlyMsg(message string, c *gin.Context) {
	Result(SUCCESS, nil, message, c)
}

// Failed 响应失败 只返回错误信息
func Failed(message string, c *gin.Context) {
	Result(FAILED, nil, message, c)
}

// FailedWithData 返回错误信息以及必要数据
func FailedWithData(data any, message string, c *gin.Context) {
	Result(FAILED, data, message, c)
}

// CustomResult 自定义返回状态以及相关数据, 用于异常返回情况
func CustomResult(statusCode int, code int, data any, msg string, c *gin.Context) {
	c.JSON(statusCode, Response{
		Code: code,
		Data: data,
		Msg:  msg,
	})
}

// GetPage 统计当前查询的总数和页数。
func GetPage(query *gorm.DB, page *Page) error {
	if page.PageSize <= 0 {
		page.PageSize = 20
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	page.Total = int(count)
	if page.Total <= 0 {
		page.PageCount = 0
		return nil
	}
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	return nil
}

func firstQuery(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(c.Query(key)); v != "" {
			return v
		}
	}
	return ""
}

// GetPageParams 从 Gin 上下文中提取通用的分页参数。
// 页码认 current、page、pg。每页条数认 pageSize、pagesize、limit、size。
func GetPageParams(c *gin.Context) *Page {
	current, _ := strconv.Atoi(firstQuery(c, "current", "page", "pg"))
	if current <= 0 {
		current = 1
	}

	pageSize, _ := strconv.Atoi(firstQuery(c, "pageSize", "pagesize", "limit", "size"))
	if pageSize <= 0 {
		pageSize = 20
	} else if pageSize > 500 {
		pageSize = 500
	}

	return &Page{
		Current:  current,
		PageSize: pageSize,
	}
}
