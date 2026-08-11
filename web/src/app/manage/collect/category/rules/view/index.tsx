"use client";

import { useCallback, useState } from "react";
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
      <ManagePageHeader title="分类规则" description="将主采集站来源分类合并到前台展示分类。" />

      <RuleWorkspace ruleTotals={ruleTotals} onRuleTotalsChange={handleRuleTotalsChange} />
    </div>
  );
}
