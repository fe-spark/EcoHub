"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Button, Card, Descriptions, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { TreeProps } from "antd/es/tree";
import { PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import CategoryTreeCard from "./category-tree-card";
import RuleEditorModal from "./rule-editor-modal";
import styles from "./index.module.less";
import {
  CATEGORY_GROUPS,
  ROOT_GROUP,
  SUB_GROUP,
  buildTreeData,
  cloneTree,
  collectStats,
  removeTreeNode,
  normalizeRuleRecord,
  normalizeTree,
  parseRuleList,
  resolveDropOffset,
  resolveGroupLabel,
  serializeTree,
  moveNodeWithinList,
  type ClassTreeDataNode,
  type ConflictCheckResult,
  type FilmClassNode,
  type MappingRuleRecord,
  type PagingState,
  type RuleFormValues,
  type TreeDropNode,
} from "./types";

export default function CategoryWorkspacePageView() {
  const { message } = useAppMessage();
  const [classTree, setClassTree] = useState<FilmClassNode[]>([]);
  const [originalTree, setOriginalTree] = useState<FilmClassNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);
  const [loadingTree, setLoadingTree] = useState(false);
  const [savingTree, setSavingTree] = useState(false);
  const [resettingTree, setResettingTree] = useState(false);
  const [resetConfirmOpen, setResetConfirmOpen] = useState(false);

  const [ruleGroup, setRuleGroup] = useState<string>(ROOT_GROUP);
  const [keyword, setKeyword] = useState("");
  const [rulesLoading, setRulesLoading] = useState(false);
  const [rulesSubmitting, setRulesSubmitting] = useState(false);
  const [rules, setRules] = useState<MappingRuleRecord[]>([]);
  const [paging, setPaging] = useState<PagingState>({ current: 1, pageSize: 10, total: 0 });
  const [ruleTotals, setRuleTotals] = useState<Record<string, number>>({ [ROOT_GROUP]: 0, [SUB_GROUP]: 0 });
  const [editorVisible, setEditorVisible] = useState(false);
  const [editingRule, setEditingRule] = useState<MappingRuleRecord | null>(null);
  const [checkingConflict, setCheckingConflict] = useState(false);
  const [conflictRules, setConflictRules] = useState<ConflictCheckResult[]>([]);
  const [ruleForm] = Form.useForm<RuleFormValues>();
  const watchedGroup = Form.useWatch("group", ruleForm);
  const watchedRaw = Form.useWatch("raw", ruleForm);
  const watchedMatchType = Form.useWatch("matchType", ruleForm);

  const stats = useMemo(() => collectStats(classTree), [classTree]);
  const hasPendingChanges = useMemo(
    () => JSON.stringify(serializeTree(classTree)) !== JSON.stringify(serializeTree(originalTree)),
    [classTree, originalTree],
  );
  const treeData = useMemo(() => buildTreeData(classTree), [classTree]);
  const hiddenStatus = stats.hidden === 0 ? "正常" : `待检查 ${stats.hidden}`;
  const ruleSummary = `${ruleTotals[ROOT_GROUP] || 0} / ${ruleTotals[SUB_GROUP] || 0}`;
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
      const samples = ["电视剧", "连续剧", "国产剧", "日本剧", "日剧", "国漫", "国产动漫", "日韩综艺", "体育赛事", "篮球"];
      return {
        valid: true,
        matches: samples.filter((item) => tester.test(item)),
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

  const fetchFilmClassTree = useCallback(async () => {
    setLoadingTree(true);
    try {
      const resp = await ApiGet("/manage/film/class/tree");
      if (resp.code !== 0) {
        message.error(resp.msg || "分类数据加载失败");
        return;
      }
      const tree = normalizeTree((resp.data?.children || []) as FilmClassNode[]);
      setClassTree(tree);
      setOriginalTree(cloneTree(tree));
      setExpandedKeys([]);
    } finally {
      setLoadingTree(false);
    }
  }, [message]);

  const fetchRuleTotals = useCallback(async () => {
    try {
      const [rootResp, subResp] = await Promise.all(
        CATEGORY_GROUPS.map((group) =>
          ApiGet("/manage/mapping/rule/list", {
            current: 1,
            pageSize: 1,
            group,
            keyword: "",
          }),
        ),
      );
      const rootData = parseRuleList(rootResp, 1, 1);
      const subData = parseRuleList(subResp, 1, 1);
      setRuleTotals({
        [ROOT_GROUP]: rootData.paging.total,
        [SUB_GROUP]: subData.paging.total,
      });
    } catch {
      // 忽略统计拉取失败，避免影响主流程。
    }
  }, []);

  const fetchRules = useCallback(
    async (page: number, pageSize: number, nextKeyword: string, nextGroup: string) => {
      setRulesLoading(true);
      try {
        const resp = await ApiGet("/manage/mapping/rule/list", {
          current: page,
          pageSize,
          group: nextGroup,
          keyword: nextKeyword.trim(),
        });
        if (resp.code !== 0) {
          message.error(resp.msg || "分类规则加载失败");
          return;
        }
        const parsed = parseRuleList(resp, page, pageSize);
        setRules(parsed.rules.filter((item) => CATEGORY_GROUPS.includes(item.group)));
        setPaging(parsed.paging);
      } finally {
        setRulesLoading(false);
      }
    },
    [message],
  );

  useEffect(() => {
    void fetchFilmClassTree();
    void fetchRuleTotals();
  }, [fetchFilmClassTree, fetchRuleTotals]);

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
        const resp = await ApiPost("/manage/mapping/rule/check", {
          id: editingRule?.id,
          group,
          raw,
          matchType,
        });
        if (resp.code === 0) {
          const list = Array.isArray(resp.data?.rules) ? resp.data.rules : Array.isArray(resp.data) ? resp.data : [];
          setConflictRules(list.map((item: Record<string, unknown>) => normalizeRuleRecord(item)));
        }
      } finally {
        setCheckingConflict(false);
      }
    }, 250);
    return () => window.clearTimeout(timer);
  }, [editorVisible, editingRule?.id, watchedGroup, watchedRaw, watchedMatchType]);

  const handleResetConfirm = async () => {
    setResettingTree(true);
    try {
      const resp = await ApiPost("/manage/film/class/collect", {});
      if (resp.code !== 0) {
        message.error(resp.msg || "重置分类失败");
        return;
      }
      message.success(resp.msg || "分类已重置");
      setResetConfirmOpen(false);
      await fetchFilmClassTree();
    } finally {
      setResettingTree(false);
    }
  };

  const saveTree = async () => {
    setSavingTree(true);
    try {
      const resp = await ApiPost("/manage/film/class/tree/save", { children: classTree });
      if (resp.code !== 0) {
        message.error(resp.msg || "保存分类变更失败");
        return;
      }
      message.success(resp.msg || "分类变更已保存");
      await fetchFilmClassTree();
    } finally {
      setSavingTree(false);
    }
  };

  const queueDeleteClass = (id: number) => {
    setClassTree((prev) => normalizeTree(removeTreeNode(prev, id)));
    message.success("删除操作已加入待保存变更");
  };

  const handleTreeDrop: TreeProps<ClassTreeDataNode>["onDrop"] = (info) => {
    const dragNode = info.dragNode as TreeDropNode;
    const dropNode = info.node as TreeDropNode;
    const dragId = dragNode.rawNode.id;
    const dropId = dropNode.rawNode.id;
    const dropOffset = resolveDropOffset(info.dropPosition, String(info.node.pos));
    const placeAfter = info.dropToGap ? dropOffset > 0 : true;

    if (dragNode.rawNode.pid === 0 && dropNode.rawNode.pid === 0) {
      setClassTree((prev) => {
        const moved = moveNodeWithinList(prev, dragId, dropId, placeAfter);
        return moved ? normalizeTree(moved) : prev;
      });
      return;
    }

    if (dragNode.rawNode.pid > 0 && dragNode.rawNode.pid === dropNode.rawNode.pid) {
      setClassTree((prev) => {
        const next = cloneTree(prev);
        const root = next.find((item) => item.id === dragNode.rawNode.pid);
        if (!root) {
          return prev;
        }
        const siblings = root.children || [];
        const moved = moveNodeWithinList(siblings, dragId, dropId, placeAfter);
        if (!moved) {
          return prev;
        }
        root.children = moved;
        return normalizeTree(next);
      });
    }
  };

  const allowTreeDrop: TreeProps<ClassTreeDataNode>["allowDrop"] = ({ dragNode, dropNode }) => {
    const dragRaw = (dragNode as TreeDropNode).rawNode;
    const dropRaw = (dropNode as TreeDropNode).rawNode;
    if (dragRaw.pid === 0) {
      return dropRaw.pid === 0;
    }
    return dragRaw.pid > 0 && dragRaw.pid === dropRaw.pid;
  };

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
    ruleForm.setFieldsValue({
      group: ruleGroup || ROOT_GROUP,
      raw: "",
      target: "",
      matchType: "exact",
      remarks: "",
    });
  };

  const handleRuleSubmit = async () => {
    const values = await ruleForm.validateFields();
    setRulesSubmitting(true);
    try {
      const payload = {
        ...(editingRule ? { id: editingRule.id } : {}),
        group: values.group,
        raw: values.raw.trim(),
        target: values.target.trim(),
        matchType: values.matchType,
        remarks: values.remarks?.trim() || "",
      };
      const resp = await ApiPost(editingRule ? "/manage/mapping/rule/update" : "/manage/mapping/rule/add", payload);
      if (resp.code !== 0) {
        message.error(resp.msg || "保存分类规则失败");
        return;
      }
      message.success(resp.msg || "分类规则已保存");
      closeRuleEditor();
      await Promise.all([fetchRules(paging.current, paging.pageSize, keyword, ruleGroup), fetchRuleTotals(), fetchFilmClassTree()]);
    } finally {
      setRulesSubmitting(false);
    }
  };

  const handleDeleteRule = async (id: number) => {
    const resp = await ApiPost("/manage/mapping/rule/del", { id });
    if (resp.code !== 0) {
      message.error(resp.msg || "删除分类规则失败");
      return;
    }
    message.success(resp.msg || "分类规则已删除");
    const nextPage = paging.current > 1 && rules.length === 1 ? paging.current - 1 : paging.current;
    await Promise.all([fetchRules(nextPage, paging.pageSize, keyword, ruleGroup), fetchRuleTotals(), fetchFilmClassTree()]);
  };

  const ruleColumns: ColumnsType<MappingRuleRecord> = [
    {
      title: "分组",
      dataIndex: "group",
      width: 132,
      render: (value: string) => <Tag color={value === ROOT_GROUP ? "gold" : "blue"}>{resolveGroupLabel(value)}</Tag>,
    },
    {
      title: "原始值",
      dataIndex: "raw",
      render: (value: string) => <Typography.Text strong>{value}</Typography.Text>,
    },
    {
      title: "匹配方式",
      dataIndex: "matchType",
      width: 96,
      render: (value: string) => (value === "regex" ? "正则" : "精确"),
    },
    {
      title: "目标值",
      dataIndex: "target",
      render: (value: string) =>
        value ? <Tag color="processing">{value}</Tag> : <Typography.Text type="secondary">未设置</Typography.Text>,
    },
    {
      title: "说明",
      dataIndex: "remarks",
      render: (value: string) => value || <Typography.Text type="secondary">暂无说明</Typography.Text>,
    },
    {
      title: "操作",
      key: "action",
      width: 140,
      fixed: "right",
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
    <div className={styles.pageBody}>
      <ManagePageHeader
        title="分类管理"
        description="维护分类规则与业务分类树。"
      />

      <Card size="small">
        <Descriptions size="small" column={{ xs: 1, md: 2, xl: 4 }}>
          <Descriptions.Item label="分类节点">{stats.total}</Descriptions.Item>
          <Descriptions.Item label="一级 / 二级">{stats.roots} / {stats.children}</Descriptions.Item>
          <Descriptions.Item label="隐藏分类">
            <Tag color={stats.hidden === 0 ? "success" : "warning"}>{hiddenStatus}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="一级规则 / 二级规则">
            <Tag color="processing">{ruleSummary}</Tag>
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Space size={[8, 8]} wrap>
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
        <Button type="primary" onClick={() => void fetchRules(1, paging.pageSize, keyword, ruleGroup)}>
          搜索
        </Button>
      </Space>

      <div className={styles.workspace}>
        <div className={styles.ruleWorkspace}>
        <Table<MappingRuleRecord>
          rowKey="id"
          columns={ruleColumns}
          dataSource={rules}
          loading={rulesLoading}
          title={() => (
            <div className={styles.tableToolbar}>
              <div className={styles.tableTitle}>分类规则</div>
              <Space wrap className={styles.tableActions}>
                <Button icon={<ReloadOutlined />} onClick={() => void Promise.all([fetchRules(1, paging.pageSize, keyword, ruleGroup), fetchRuleTotals()])}>
                  刷新规则
                </Button>
                <Button type="primary" icon={<PlusOutlined />} onClick={openCreateModal}>
                  新增规则
                </Button>
              </Space>
            </div>
          )}
          pagination={{
            current: paging.current,
            pageSize: paging.pageSize,
            total: paging.total,
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 条分类规则`,
            onChange: (page, pageSize) => void fetchRules(page, pageSize, keyword, ruleGroup),
          }}
        />
        </div>

        <CategoryTreeCard
          classTree={classTree}
          treeData={treeData}
          expandedKeys={expandedKeys}
          loadingTree={loadingTree}
          savingTree={savingTree}
          resettingTree={resettingTree}
          hasPendingChanges={hasPendingChanges}
          onRefresh={() => void fetchFilmClassTree()}
          onReset={() => setResetConfirmOpen(true)}
          onSave={() => void saveTree()}
          onExpand={(keys) => setExpandedKeys(keys)}
          onDrop={handleTreeDrop}
          allowDrop={allowTreeDrop}
          onDelete={queueDeleteClass}
        />
      </div>

      <Modal
        title="确认重置分类？"
        open={resetConfirmOpen}
        okText="确认重置"
        cancelText="取消"
        confirmLoading={resettingTree}
        onOk={() => void handleResetConfirm()}
        onCancel={() => setResetConfirmOpen(false)}
      >
        该操作会清空当前分类的业务名称、显示状态、排序等设置，并重新获取主站原始分类。
      </Modal>

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
