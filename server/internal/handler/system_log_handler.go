package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type SystemLogHandler struct{}

var SystemLogHd = new(SystemLogHandler)

func allowSystemLogAdmin(c *gin.Context) bool {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
		return false
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "权限不足，仅超级管理员可查看系统日志", c)
		return false
	}
	return true
}

func (h *SystemLogHandler) Delta(c *gin.Context) {
	if !allowSystemLogAdmin(c) {
		return
	}

	after, _ := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10000"))
	if after <= 0 {
		lines, _ := strconv.Atoi(c.DefaultQuery("lines", "500"))
		entries, nextSeq, err := syslog.RecentEntries(lines)
		if err != nil {
			dto.Failed(fmt.Sprintf("系统日志读取失败: %v", err), c)
			return
		}
		dto.Success(gin.H{"entries": entries, "nextSeq": nextSeq, "expired": false}, "系统日志增量获取成功", c)
		return
	}

	result := syslog.DeltaAfter(after, limit)
	dto.Success(gin.H{
		"entries": result.Entries,
		"nextSeq": result.NextSeq,
		"minSeq":  result.MinSeq,
		"expired": result.Expired,
	}, "系统日志增量获取成功", c)
}

// Stream 推送运行日志。after<=0 先发最近快照，之后只推新增；连接断开不写日志。
func (h *SystemLogHandler) Stream(c *gin.Context) {
	if !allowSystemLogAdmin(c) {
		return
	}
	lines, _ := strconv.Atoi(c.DefaultQuery("lines", "500"))
	after, _ := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	if after < 0 {
		after = 0
	}
	stream, ok := openSSE(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	wake := syslog.Subscribe(ctx)
	var err error
	if after <= 0 {
		after, err = sendLogSnapshot(stream, lines)
		if err != nil {
			return
		}
	} else {
		after, err = sendLogDelta(stream, after)
		if err != nil {
			return
		}
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case _, open := <-wake:
			if !open {
				return
			}
			after, err = sendLogDelta(stream, after)
			if err != nil {
				return
			}
		case <-heartbeat.C:
			if err = stream.Comment("ping"); err != nil {
				return
			}
		}
	}
}

func sendLogSnapshot(stream *sseWriter, lines int) (int64, error) {
	entries, next, err := syslog.RecentEntries(lines)
	if err != nil {
		_ = stream.SendData(gin.H{
			"entries": []syslog.Entry{},
			"nextSeq": 0,
			"minSeq":  0,
			"expired": false,
			"reset":   true,
			"error":   err.Error(),
		})
		return 0, err
	}
	var minSeq int64
	if len(entries) > 0 {
		minSeq = entries[0].Seq
	}
	if err := sendLogBatch(stream, entries, next, minSeq, false, true); err != nil {
		return next, err
	}
	return next, nil
}

// sendLogDelta 把游标之后的日志推完。缓冲被截断时整段替换，单批放不下时接着推。
func sendLogDelta(stream *sseWriter, cursor int64) (int64, error) {
	for {
		result := syslog.DeltaAfter(cursor, 10000)
		if result.Expired {
			if err := sendLogBatch(stream, result.Entries, result.NextSeq, result.MinSeq, true, true); err != nil {
				return cursor, err
			}
			return result.NextSeq, nil
		}
		if len(result.Entries) == 0 {
			if result.NextSeq > cursor {
				return result.NextSeq, nil
			}
			return cursor, nil
		}
		last := result.Entries[len(result.Entries)-1].Seq
		if last <= cursor {
			return cursor, nil
		}
		next := result.NextSeq
		truncated := last < result.NextSeq
		if truncated {
			next = last
		}
		if err := sendLogBatch(stream, result.Entries, next, result.MinSeq, false, false); err != nil {
			return cursor, err
		}
		cursor = next
		if !truncated {
			return cursor, nil
		}
	}
}

func sendLogBatch(stream *sseWriter, entries []syslog.Entry, nextSeq, minSeq int64, expired, reset bool) error {
	if entries == nil {
		entries = []syslog.Entry{}
	}
	return stream.SendData(gin.H{
		"entries": entries,
		"nextSeq": nextSeq,
		"minSeq":  minSeq,
		"expired": expired,
		"reset":   reset,
	})
}
