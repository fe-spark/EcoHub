import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button, Form, Input, Pagination, Popconfirm, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { PlusOutlined, ReloadOutlined, SearchOutlined, SlidersOutlined } from "@ant-design/icons";
import {
  ContentFilterBar,
  ContentPageShell,
  ContentSectionCard,
  ContentSourceSelect,
} from "@/app/manage/components/content-shell";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { usePrimarySource } from "@/lib/manage-primary";
import {
  deleteCategoryRule,
  getCategoryRuleContext,
  getCategoryRules,
  getFilmClassTree,
  saveCategoryRule,
  type CategoryRuleContextData,
} from "./api";
import RuleEditorDrawer from "./rule-editor-drawer";
import { buildFinalCategoryPreview, buildNextRawInput, findLocalRuleConflicts } from "./rule-preview-engine";
import styles from "./index.module.less";
import {
  ALL_CATEGORY_GROUP,
  CATEGORY_FILTER_GROUPS,
  CATEGORY_GROUPS,
  ROOT_GROUP,
  SUB_GROUP,
  resolveGroupLabel,
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
  const { canWrite } = useManagePermission();
  const { primary } = usePrimarySource();
  const primaryId = primary?.id || "";
  const { message } = useAppMessage();
  const [ruleGroup, setRuleGroup] = useState<string>(ALL_CATEGORY_GROUP);
  const [keyword, setKeyword] = useState("");
  const [rulesLoading, setRulesLoading] = useState(false);
  const [rulesSubmitting, setRulesSubmitting] = useState(false);
  const [rules, setRules] = useState<MappingRuleRecord[]>([]);
  const [paging, setPaging] = useState<PagingState>({ current: 1, pageSize: 10, total: 0 });
  const [editorVisible, setEditorVisible] = useState(false);
  const [editingRule, setEditingRule] = useState<MappingRuleRecord | null>(null);
  const [ruleForm] = Form.useForm<RuleFormValues>();
  const watchedGroup = Form.useWatch("group", ruleForm);
  const watchedRaw = Form.useWatch("raw", ruleForm);
  const watchedTarget = Form.useWatch("target", ruleForm);
  const watchedMatchType = Form.useWatch("matchType", ruleForm);
  const [sources, setSources] = useState<{ id: string; name: string; state?: boolean }[]>([]);
  const [sourceId, setSourceId] = useState("");
  const [contextLoading, setContextLoading] = useState(false);
  const [ruleContexts, setRuleContexts] = useState<Record<string, CategoryRuleContextData>>({});
  const contextCacheRef = useRef<Record<string, CategoryRuleContextData>>({});

  const fetchRules = useCallback(
    async (page: number, pageSize: number, nextKeyword: string, nextGroup: string, nextSourceId: string) => {
      if (!nextSourceId) {
        setRules([]);
        setPaging({ current: 1, pageSize, total: 0 });
        onRuleTotalsChange({ [ROOT_GROUP]: 0, [SUB_GROUP]: 0 });
        return;
      }
      setRulesLoading(true);
      try {
        const { resp, parsed } = await getCategoryRules(page, pageSize, nextKeyword, nextGroup, nextSourceId);
        if (resp.code !== 0) {
          message.error(resp.msg || "分类规则加载失败");
          return;
        }
        setRules(parsed.rules.filter((item) => CATEGORY_GROUPS.includes(item.group)));
        setPaging(parsed.paging);
        if (parsed.totals) {
          onRuleTotalsChange(parsed.totals);
        }
      } finally {
        setRulesLoading(false);
      }
    },
    [message, onRuleTotalsChange],
  );

  const ensureRuleContext = useCallback(
    async (targetSourceId: string, force = false) => {
      if (!targetSourceId) return;
      if (!force && contextCacheRef.current[targetSourceId]) {
        return;
      }
      setContextLoading(true);
      try {
        const { resp, context } = await getCategoryRuleContext(targetSourceId);
        if (resp.code !== 0) {
          message.error(resp.msg || "当前分类拉取失败");
          return;
        }
        contextCacheRef.current[targetSourceId] = context;
        setRuleContexts((prev) => ({ ...prev, [targetSourceId]: context }));
      } finally {
        setContextLoading(false);
      }
    },
    [message],
  );

  const invalidateRuleContext = useCallback((targetSourceId: string) => {
    if (!targetSourceId) return;
    delete contextCacheRef.current[targetSourceId];
    setRuleContexts((prev) => {
      if (!(targetSourceId in prev)) return prev;
      const next = { ...prev };
      delete next[targetSourceId];
      return next;
    });
  }, []);

  useEffect(() => {
    void (async () => {
      try {
        const { sources: nextSources, defaultSourceId, currentSourceId } = await getFilmClassTree("");
        setSources(nextSources);
        setSourceId(currentSourceId || defaultSourceId);
      } catch {
        message.error("采集站列表加载失败");
      }
    })();
  }, [message]);

  useEffect(() => {
    if (!sourceId) {
      return;
    }
    void fetchRules(1, paging.pageSize, keyword, ruleGroup, sourceId);
  }, [fetchRules, keyword, paging.pageSize, ruleGroup, sourceId]);

  const activeContext = sourceId ? ruleContexts[sourceId] : undefined;

  // 弹窗打开时按需拉取一次当前分类与全量规则，输入过程中纯本地计算冲突与最终全部分类预览
  const conflictRules = useMemo(() => {
    if (!editorVisible || !activeContext) {
      return [];
    }
    return findLocalRuleConflicts(activeContext.rules, {
      editingId: editingRule?.id,
      group: String(watchedGroup || ROOT_GROUP),
      raw: String(watchedRaw || ""),
      matchType: String(watchedMatchType || "exact"),
    });
  }, [activeContext, editingRule?.id, editorVisible, watchedGroup, watchedMatchType, watchedRaw]);

  const fullPreview = useMemo(() => {
    return buildFinalCategoryPreview({
      rawCategories: activeContext?.rawCategories || [],
      savedRules: activeContext?.rules || [],
      draft: {
        editingId: editingRule?.id,
        group: String(watchedGroup || ROOT_GROUP),
        matchType: String(watchedMatchType || "exact"),
        raw: String(watchedRaw || ""),
        target: String(watchedTarget || ""),
      },
    });
  }, [activeContext, editingRule?.id, watchedGroup, watchedMatchType, watchedRaw, watchedTarget]);

  const openCreateModal = () => {
    setEditingRule(null);
    setEditorVisible(true);
    void ensureRuleContext(sourceId);
  };

  const openEditRuleModal = (record: MappingRuleRecord) => {
    setEditingRule(record);
    setEditorVisible(true);
    void ensureRuleContext(sourceId);
  };

  const closeRuleEditor = () => {
    setEditorVisible(false);
    setEditingRule(null);
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
    const defaultGroup = ruleGroup === ALL_CATEGORY_GROUP ? ROOT_GROUP : ruleGroup;
    ruleForm.setFieldsValue({ group: defaultGroup, raw: "", target: "", matchType: "exact", remarks: "" });
  };

  const handlePickRawName = (clickedName: string) => {
    const currentRaw = String(ruleForm.getFieldValue("raw") || "");
    const matchType = (ruleForm.getFieldValue("matchType") || "exact") as "exact" | "regex";
    const nextRaw = buildNextRawInput(currentRaw, matchType, clickedName);
    ruleForm.setFieldValue("raw", nextRaw);
  };

  const handleRuleSubmit = async () => {
    if (!sourceId) {
      message.error("请先选择采集站");
      return;
    }
    const values = await ruleForm.validateFields();
    setRulesSubmitting(true);
    try {
      const resp = await saveCategoryRule({
        ...(editingRule ? { id: editingRule.id } : {}),
        sourceId,
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
      invalidateRuleContext(sourceId);
      closeRuleEditor();
      // 若当前仅筛选了另一规则分组，自动切换到所保存的分组以免刚新增的规则被过滤隐藏
      const nextGroup =
        ruleGroup !== ALL_CATEGORY_GROUP && ruleGroup !== values.group ? values.group : ruleGroup;
      if (nextGroup !== ruleGroup) {
        setRuleGroup(nextGroup);
      }
      await fetchRules(1, paging.pageSize, keyword, nextGroup, sourceId);
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
    invalidateRuleContext(sourceId);
    const nextPage = paging.current > 1 && rules.length === 1 ? paging.current - 1 : paging.current;
    await fetchRules(nextPage, paging.pageSize, keyword, ruleGroup, sourceId);
  };

  const ruleColumns: ColumnsType<MappingRuleRecord> = [
    {
      title: "ID",
      dataIndex: "id",
      width: 70,
      fixed: "left",
      align: "center",
      render: (value: number) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          #{value}
        </Typography.Text>
      ),
    },
    {
      title: "规则分组",
      dataIndex: "group",
      width: 120,
      align: "center",
      render: (value: string) => (
        <Tag color={value === ROOT_GROUP ? "orange" : "blue"} variant="filled">
          {resolveGroupLabel(value)}
        </Tag>
      ),
    },
    {
      title: "原始值",
      dataIndex: "raw",
      align: "left",
      render: (value: string) => <Typography.Text strong>{value}</Typography.Text>,
    },
    {
      title: "匹配方式",
      dataIndex: "matchType",
      width: 100,
      align: "center",
      render: (value: string) => (
        <Typography.Text type="secondary">
          {value === "regex" ? "正则匹配" : "精确匹配"}
        </Typography.Text>
      ),
    },
    {
      title: "目标展示值",
      dataIndex: "target",
      align: "left",
      render: (value: string) =>
        value ? (
          <Typography.Text strong style={{ color: "var(--ant-color-primary)" }}>
            {value}
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary">未设置</Typography.Text>
        ),
    },
    {
      title: "说明",
      dataIndex: "remarks",
      align: "left",
      render: (value: string) => value || <Typography.Text type="secondary">-</Typography.Text>,
    },
    {
      title: "操作",
      key: "action",
      fixed: "right",
      align: "center",
      render: (_, record) => (
        <Space size={8}>
          <Button type="link" size="small" disabled={!canWrite} onClick={() => openEditRuleModal(record)}>
            编辑
          </Button>
          <Popconfirm title="删除该规则？" okText="删除" cancelText="取消" onConfirm={() => void handleDeleteRule(record.id)}>
            <Button type="link" size="small" danger disabled={!canWrite}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const currentSourceName =
    activeContext?.sourceName || sources.find((s) => s.id === sourceId)?.name || "";

  return (
    <ContentPageShell
      title="分类规则"
      description="把采集站的分类对应到前台显示的名称。"
      headerActions={
        <ContentSourceSelect
          sources={sources}
          sourceId={sourceId}
          primaryId={primaryId}
          onChange={(value) => setSourceId(value)}
        />
      }
    >
      <ContentFilterBar
        lead={
          <>
            <Select
              value={ruleGroup}
              options={CATEGORY_FILTER_GROUPS.map((group) => ({ value: group, label: resolveGroupLabel(group) }))}
              onChange={(value) => setRuleGroup(value)}
              className={styles.groupSelect}
            />
            <Input
              allowClear
              placeholder="搜索原始值、目标值或说明"
              value={keyword}
              onChange={(event) => setKeyword(event.target.value)}
              onPressEnter={() => void fetchRules(1, paging.pageSize, keyword, ruleGroup, sourceId)}
              className={styles.searchInput}
            />
            <Button
              type="primary"
              icon={<SearchOutlined />}
              onClick={() => void fetchRules(1, paging.pageSize, keyword, ruleGroup, sourceId)}
              className={styles.searchButton}
            >
              搜索
            </Button>
            <Button
              icon={<ReloadOutlined />}
              onClick={() => {
                setKeyword("");
                setRuleGroup(ALL_CATEGORY_GROUP);
                void fetchRules(1, paging.pageSize, "", ALL_CATEGORY_GROUP, sourceId);
              }}
            >
              重置
            </Button>
          </>
        }
      />

      <ContentSectionCard
        icon={<SlidersOutlined />}
        title="分类规则"
        count={paging.total}
        extra={
          <Tag color="processing" style={{ margin: 0 }}>
            一级 {ruleTotals[ROOT_GROUP] || 0} / 二级 {ruleTotals[SUB_GROUP] || 0}
          </Tag>
        }
        actions={
          <Space size={8} wrap>
            <Button
              icon={<ReloadOutlined />}
              onClick={() => {
                invalidateRuleContext(sourceId);
                void fetchRules(1, paging.pageSize, keyword, ruleGroup, sourceId);
              }}
            >
              刷新规则
            </Button>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              disabled={!canWrite || !sourceId}
              onClick={openCreateModal}
            >
              新增规则
            </Button>
          </Space>
        }
        footer={
          <Pagination
            current={paging.current}
            pageSize={paging.pageSize}
            total={paging.total}
            showSizeChanger
            pageSizeOptions={[10, 20, 50, 100, 500]}
            showTotal={(total) => `共 ${total} 条`}
            onChange={(page, pageSize) => void fetchRules(page, pageSize, keyword, ruleGroup, sourceId)}
          />
        }
      >
        <Table<MappingRuleRecord>
          rowKey="id"
          columns={ruleColumns}
          dataSource={rules}
          loading={rulesLoading}
          size="middle"
          pagination={false}
          scroll={{ x: "max-content" }}
        />
      </ContentSectionCard>

      <RuleEditorDrawer
        open={editorVisible}
        loading={rulesSubmitting}
        contextLoading={contextLoading}
        sourceName={currentSourceName}
        editingRule={editingRule}
        form={ruleForm}
        conflictRules={conflictRules}
        watchedGroup={watchedGroup}
        preview={fullPreview}
        onPickRawName={handlePickRawName}
        onSubmit={() => {
          if (!canWrite) return;
          void handleRuleSubmit();
        }}
        onCancel={closeRuleEditor}
        onAfterOpenChange={applyEditorValues}
      />
    </ContentPageShell>
  );
}
