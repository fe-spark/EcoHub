"use client";

import { useCallback, useState } from "react";
import { Alert, Card, Descriptions, Tag } from "antd";
import ManagePageHeader from "@/app/manage/components/page-header";
import RuleWorkspace from "../../view/rule-workspace";
import styles from "../../view/index.module.less";
import { ROOT_GROUP, SUB_GROUP } from "../../view/types";

export default function CategoryRulePageView() {
  const [ruleTotals, setRuleTotals] = useState<Record<string, number>>({ [ROOT_GROUP]: 0, [SUB_GROUP]: 0 });
  const handleRuleTotalsChange = useCallback((totals: Record<string, number>) => {
    setRuleTotals({ [ROOT_GROUP]: totals[ROOT_GROUP] || 0, [SUB_GROUP]: totals[SUB_GROUP] || 0 });
  }, []);

  return (
    <div className={styles.pageBody}>
      <ManagePageHeader title="分类规则" description="将主站来源分类合并到前台展示分类。" />

      <Card size="small">
        <Descriptions size="small" column={{ xs: 1, md: 2 }}>
          <Descriptions.Item label="一级规则">
            <Tag color="gold">{ruleTotals[ROOT_GROUP] || 0}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="二级规则">
            <Tag color="blue">{ruleTotals[SUB_GROUP] || 0}</Tag>
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Alert
        type="info"
        showIcon
        message="规则立即刷新分类映射"
        description="一级/二级规则用于把来源分类合并到目标展示分类；保存后会刷新分类树和来源映射，历史影片不重写、不重采集，查询时会按最新映射归入新分组。"
      />

      <RuleWorkspace ruleTotals={ruleTotals} onRuleTotalsChange={handleRuleTotalsChange} />
    </div>
  );
}
