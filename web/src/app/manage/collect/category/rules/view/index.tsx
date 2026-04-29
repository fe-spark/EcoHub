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
      <ManagePageHeader title="分类规则" description="配置下一轮主站采集生效的分类匹配规则。" />

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
        message="规则只影响未来采集"
        description="修改分类规则不会回写历史展示数据。需要新规则生效时，请重新执行主站全量采集。"
      />

      <RuleWorkspace ruleTotals={ruleTotals} onRuleTotalsChange={handleRuleTotalsChange} />
    </div>
  );
}
