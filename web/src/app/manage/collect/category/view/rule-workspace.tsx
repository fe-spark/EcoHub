import { useCallback, useEffect, useMemo, useState } from "react";
import { Button, Form, Input, Pagination, Popconfirm, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import { useAppMessage } from "@/lib/useAppMessage";
import {
  checkCategoryRuleConflict,
  deleteCategoryRule,
  getCategoryRules,
  getCategoryRuleTotals,
  saveCategoryRule,
} from "./api";
import RuleEditorModal from "./rule-editor-modal";
import styles from "./index.module.less";
import {
  CATEGORY_GROUPS,
  ROOT_GROUP,
  SUB_GROUP,
  regexPreviewSamples,
  resolveGroupLabel,
  type ConflictCheckResult,
  type MappingRuleRecord,
  type PagingState,
  type RuleFormValues,
} from "./types";

interface RuleWorkspaceProps {
  ruleTotals: Record<string, number>;
  onRuleTotalsChange: (totals: Record<string, number>) => void;
}

export default function RuleWorkspace(props: RuleWorkspaceProps) {
  const { ruleTotals, onRuleTotalsChange } = props;
  const { message } = useAppMessage();
  const [ruleGroup, setRuleGroup] = useState<string>(ROOT_GROUP);
  const [keyword, setKeyword] = useState("");
  const [rulesLoading, setRulesLoading] = useState(false);
  const [rulesSubmitting, setRulesSubmitting] = useState(false);
  const [rules, setRules] = useState<MappingRuleRecord[]>([]);
  const [paging, setPaging] = useState<PagingState>({ current: 1, pageSize: 10, total: 0 });
  const [editorVisible, setEditorVisible] = useState(false);
  const [editingRule, setEditingRule] = useState<MappingRuleRecord | null>(null);
  const [checkingConflict, setCheckingConflict] = useState(false);
  const [conflictRules, setConflictRules] = useState<ConflictCheckResult[]>([]);
  const [ruleForm] = Form.useForm<RuleFormValues>();
  const watchedGroup = Form.useWatch("group", ruleForm);
  const watchedRaw = Form.useWatch("raw", ruleForm);
  const watchedMatchType = Form.useWatch("matchType", ruleForm);

  const regexPreview = useMemo(() => {
    if (watchedMatchType !== "regex") {
      return { valid: true, matches: [] as string[], error: "" };
    }
    const source = String(watchedRaw || "").trim();
    if (!source) {
      return { valid: true, matches: [] as string[], error: "" };
    }
    try {
      const tester = new RegExp(source);
      return {
        valid: true,
        matches: regexPreviewSamples.filter((item) => tester.test(item)),
        error: "",
      };
    } catch (error) {
      return {
        valid: false,
        matches: [] as string[],
        error: error instanceof Error ? error.message : "正则表达式无效",
      };
    }
  }, [watchedMatchType, watchedRaw]);

  const fetchRuleTotals = useCallback(async () => {
    try {
      onRuleTotalsChange(await getCategoryRuleTotals());
    } catch {
      // 规则统计只用于摘要展示，失败不阻断主流程。
    }
  }, [onRuleTotalsChange]);

  const fetchRules = useCallback(
    async (page: number, pageSize: number, nextKeyword: string, nextGroup: string) => {
      setRulesLoading(true);
      try {
        const { resp, parsed } = await getCategoryRules(page, pageSize, nextKeyword, nextGroup);
        if (resp.code !== 0) {
          message.error(resp.msg || "分类规则加载失败");
          return;
        }
        setRules(parsed.rules.filter((item) => CATEGORY_GROUPS.includes(item.group)));
        setPaging(parsed.paging);
      } finally {
        setRulesLoading(false);
      }
    },
    [message],
  );

  useEffect(() => {
    void fetchRuleTotals();
  }, [fetchRuleTotals]);

  useEffect(() => {
    void fetchRules(1, paging.pageSize, keyword, ruleGroup);
  }, [fetchRules, keyword, paging.pageSize, ruleGroup]);

  useEffect(() => {
    if (!editorVisible) {
      setCheckingConflict(false);
      setConflictRules([]);
      return;
    }
    const group = String(watchedGroup || "").trim();
    const raw = String(watchedRaw || "").trim();
    const matchType = String(watchedMatchType || "").trim();
    if (!group || !raw || !matchType) {
      setCheckingConflict(false);
      setConflictRules([]);
      return;
    }
    const timer = window.setTimeout(async () => {
      setCheckingConflict(true);
      try {
        const { resp, rules: conflictList } = await checkCategoryRuleConflict({ id: editingRule?.id, group, raw, matchType });
        if (resp.code === 0) {
          setConflictRules(conflictList);
        }
      } finally {
        setCheckingConflict(false);
      }
    }, 250);
    return () => window.clearTimeout(timer);
  }, [editorVisible, editingRule?.id, watchedGroup, watchedRaw, watchedMatchType]);

  const openCreateModal = () => {
    setEditingRule(null);
    setEditorVisible(true);
  };

  const openEditRuleModal = (record: MappingRuleRecord) => {
    setEditingRule(record);
    setEditorVisible(true);
  };

  const closeRuleEditor = () => {
    setEditorVisible(false);
    setEditingRule(null);
    setConflictRules([]);
    ruleForm.resetFields();
  };

  const applyEditorValues = (open: boolean) => {
    if (!open) {
      return;
    }
    if (editingRule) {
      ruleForm.setFieldsValue({
        group: editingRule.group,
        raw: editingRule.raw,
        target: editingRule.target,
        matchType: editingRule.matchType as "exact" | "regex",
        remarks: editingRule.remarks,
      });
      return;
    }
    ruleForm.setFieldsValue({ group: ruleGroup || ROOT_GROUP, raw: "", target: "", matchType: "exact", remarks: "" });
  };

  const handleRuleSubmit = async () => {
    const values = await ruleForm.validateFields();
    setRulesSubmitting(true);
    try {
      const resp = await saveCategoryRule({
        ...(editingRule ? { id: editingRule.id } : {}),
        group: values.group,
        raw: values.raw.trim(),
        target: values.target.trim(),
        matchType: values.matchType,
        remarks: values.remarks?.trim() || "",
      });
      if (resp.code !== 0) {
        message.error(resp.msg || "保存分类规则失败");
        return;
      }
      message.success(resp.msg || "分类规则已保存");
      closeRuleEditor();
      await Promise.all([fetchRules(paging.current, paging.pageSize, keyword, ruleGroup), fetchRuleTotals()]);
    } finally {
      setRulesSubmitting(false);
    }
  };

  const handleDeleteRule = async (id: number) => {
    const resp = await deleteCategoryRule(id);
    if (resp.code !== 0) {
      message.error(resp.msg || "删除分类规则失败");
      return;
    }
    message.success(resp.msg || "分类规则已删除");
    const nextPage = paging.current > 1 && rules.length === 1 ? paging.current - 1 : paging.current;
    await Promise.all([fetchRules(nextPage, paging.pageSize, keyword, ruleGroup), fetchRuleTotals()]);
  };

  const ruleColumns: ColumnsType<MappingRuleRecord> = [
    { title: "ID", dataIndex: "id", width: 80, fixed: "left", align: "center", render: (value: number) => <Tag color="purple">{value}</Tag> },
    {
      title: "分组",
      dataIndex: "group",
      align: "center",
      render: (value: string) => <Tag color={value === ROOT_GROUP ? "gold" : "blue"}>{resolveGroupLabel(value)}</Tag>,
    },
    { title: "原始值", dataIndex: "raw", align: "left", render: (value: string) => <Typography.Text strong>{value}</Typography.Text> },
    { title: "匹配方式", dataIndex: "matchType", align: "center", render: (value: string) => (value === "regex" ? "正则" : "精确") },
    {
      title: "目标值",
      dataIndex: "target",
      align: "left",
      render: (value: string) => (value ? <Tag color="processing">{value}</Tag> : <Typography.Text type="secondary">未设置</Typography.Text>),
    },
    { title: "说明", dataIndex: "remarks", align: "left", render: (value: string) => value || <Typography.Text type="secondary">暂无说明</Typography.Text> },
    {
      title: "操作",
      key: "action",
      fixed: "right",
      align: "center",
      render: (_, record) => (
        <Space size={8}>
          <Button type="link" size="small" onClick={() => openEditRuleModal(record)}>
            编辑
          </Button>
          <Popconfirm title="确认删除该规则？" okText="删除" cancelText="取消" onConfirm={() => void handleDeleteRule(record.id)}>
            <Button type="link" size="small" danger>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div className={styles.ruleWorkspace}>
      <Space size={[8, 8]} wrap className={styles.filterBar}>
        <Select
          value={ruleGroup}
          options={CATEGORY_GROUPS.map((group) => ({ value: group, label: resolveGroupLabel(group) }))}
          onChange={(value) => setRuleGroup(value)}
          className={styles.groupSelect}
        />
        <Input
          allowClear
          placeholder="搜索原始值、目标值或说明"
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          onPressEnter={() => void fetchRules(1, paging.pageSize, keyword, ruleGroup)}
          className={styles.searchInput}
        />
        <Button type="primary" onClick={() => void fetchRules(1, paging.pageSize, keyword, ruleGroup)} className={styles.searchButton}>
          搜索
        </Button>
      </Space>

      <Table<MappingRuleRecord>
        rowKey="id"
        columns={ruleColumns}
        dataSource={rules}
        loading={rulesLoading}
        size="middle"
        pagination={false}
        scroll={{ x: "max-content" }}
        title={() => (
          <div className={styles.tableHeader}>
            <div className={styles.tableTitle}>分类规则</div>
            <Space size={[8, 8]} wrap className={styles.tableActions}>
              <Tag color="processing">一级 {ruleTotals[ROOT_GROUP] || 0} / 二级 {ruleTotals[SUB_GROUP] || 0}</Tag>
              <Button icon={<ReloadOutlined />} onClick={() => void Promise.all([fetchRules(1, paging.pageSize, keyword, ruleGroup), fetchRuleTotals()])}>
                刷新规则
              </Button>
              <Button type="primary" icon={<PlusOutlined />} onClick={openCreateModal}>
                新增规则
              </Button>
            </Space>
          </div>
        )}
        footer={() => (
          <div className={styles.pagination}>
            <Pagination
              current={paging.current}
              pageSize={paging.pageSize}
              total={paging.total}
              showSizeChanger
              pageSizeOptions={[10, 20, 50, 100, 500]}
              showTotal={(total) => `共 ${total} 条`}
              onChange={(page, pageSize) => void fetchRules(page, pageSize, keyword, ruleGroup)}
            />
          </div>
        )}
      />

      <RuleEditorModal
        open={editorVisible}
        loading={rulesSubmitting}
        editingRule={editingRule}
        form={ruleForm}
        conflictRules={conflictRules}
        checkingConflict={checkingConflict}
        watchedMatchType={watchedMatchType}
        regexPreview={regexPreview}
        onSubmit={() => void handleRuleSubmit()}
        onCancel={closeRuleEditor}
        onAfterOpenChange={applyEditorValues}
      />
    </div>
  );
}
