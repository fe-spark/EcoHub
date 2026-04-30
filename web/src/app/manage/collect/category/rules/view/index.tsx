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
        message="保存后会刷新展示分类和来源映射，不会重写历史影片。"
      />

      <RuleWorkspace ruleTotals={ruleTotals} onRuleTotalsChange={handleRuleTotalsChange} />
    </div>
  );
}
