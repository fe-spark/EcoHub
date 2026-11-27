package mapping

import (
	"fmt"

	"server/internal/model"
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

type MappingHandler struct{}

var MappingHd = new(MappingHandler)

// MappingRuleGroups 获取映射规则分组列表
func (h *MappingHandler) MappingRuleGroups(c *gin.Context) {
	dto.Success(MappingSvc.ListMappingRuleGroups(), "映射规则分组获取成功", c)
}

// MappingRuleList 获取映射规则分页列表
func (h *MappingHandler) MappingRuleList(c *gin.Context) {
	result, err := MappingSvc.ListMappingRules(
		c.DefaultQuery("group", ""),
		c.DefaultQuery("keyword", ""),
		c.DefaultQuery("sourceId", ""),
		dto.GetPageParams(c),
	)
	if err != nil {
		dto.Failed(fmt.Sprintf("映射规则获取失败: %v", err), c)
		return
	}
	dto.Success(result, "映射规则获取成功", c)
}

// MappingRuleReload 重载映射规则缓存
func (h *MappingHandler) MappingRuleReload(c *gin.Context) {
	MappingSvc.ReloadMappingRules()
	dto.SuccessOnlyMsg("映射规则已重载", c)
}

// MappingRuleContext 获取指定采集站的原始分类及已保存分类规则，供弹窗本地实时预览
func (h *MappingHandler) MappingRuleContext(c *gin.Context) {
	result, err := MappingSvc.GetMappingRuleContext(c.Query("sourceId"))
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(result, "映射规则上下文获取成功", c)
}

// MappingRuleSave 保存映射规则（支持新增或更新）
func (h *MappingHandler) MappingRuleSave(c *gin.Context) {
	var rule model.MappingRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		dto.Failed("映射规则参数异常", c)
		return
	}
	if rule.ID > 0 {
		if err := MappingSvc.UpdateMappingRule(rule); err != nil {
			dto.Failed(fmt.Sprintf("映射规则更新失败: %v", err), c)
			return
		}
		dto.SuccessOnlyMsg("映射规则更新成功", c)
		return
	}
	if err := MappingSvc.CreateMappingRule(rule); err != nil {
		dto.Failed(fmt.Sprintf("映射规则新增失败: %v", err), c)
		return
	}
	dto.SuccessOnlyMsg("映射规则新增成功", c)
}

// MappingRuleDelete 批量删除映射规则
func (h *MappingHandler) MappingRuleDelete(c *gin.Context) {
	var req struct {
		Ids []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("映射规则删除参数异常", c)
		return
	}
	cleanIds := make([]uint, 0, len(req.Ids))
	for _, id := range req.Ids {
		if id > 0 {
			cleanIds = append(cleanIds, id)
		}
	}
	if len(cleanIds) == 0 {
		dto.Failed("缺少需要删除的规则标识", c)
		return
	}
	if err := MappingSvc.DeleteMappingRules(cleanIds); err != nil {
		dto.Failed(fmt.Sprintf("映射规则删除失败: %v", err), c)
		return
	}
	dto.SuccessOnlyMsg("映射规则删除成功", c)
}
