"use client";

import { useCallback, useState } from "react";
import RuleWorkspace from "../../view/rule-workspace";
import { ROOT_GROUP, SUB_GROUP } from "../../view/types";

export default function CategoryRulePageView() {
  const [ruleTotals, setRuleTotals] = useState<Record<string, number>>({ [ROOT_GROUP]: 0, [SUB_GROUP]: 0 });
  const handleRuleTotalsChange = useCallback((totals: Record<string, number>) => {
    setRuleTotals({ [ROOT_GROUP]: totals[ROOT_GROUP] || 0, [SUB_GROUP]: totals[SUB_GROUP] || 0 });
  }, []);

  return <RuleWorkspace ruleTotals={ruleTotals} onRuleTotalsChange={handleRuleTotalsChange} />;
}
