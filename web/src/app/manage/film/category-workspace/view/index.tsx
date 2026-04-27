"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tree,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import type { TreeDataNode, TreeProps } from "antd/es/tree";
import {
  DeleteOutlined,
  EditOutlined,
  EyeInvisibleOutlined,
  EyeOutlined,
  HolderOutlined,
  PlusOutlined,
  ReloadOutlined,
  SaveOutlined,
} from "@ant-design/icons";
import ManagePageShell from "@/app/manage/components/page-shell";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import styles from "./index.module.less";

interface FilmClassNode {
  id: number;
  pid: number;
  name: string;
  alias?: string;
  show: boolean;
  sort?: number;
  children?: FilmClassNode[];
}

interface MappingRuleRecord {
  id: number;
  group: string;
  raw: string;
  target: string;
  matchType: string;
  remarks: string;
}

interface PagingState {
  current: number;
  pageSize: number;
  total: number;
}

interface ConflictCheckResult {
  id: number;
  group: string;
  raw: string;
  target: string;
  matchType: string;
}

interface RuleFormValues {
  group: string;
  raw: string;
  target: string;
  matchType: "exact" | "regex";
  remarks?: string;
}

interface ClassTreeDataNode extends TreeDataNode {
  key: string;
  title: string;
  rawNode: FilmClassNode;
  children?: ClassTreeDataNode[];
}

const ROOT_GROUP = "CategoryRoot";
const SUB_GROUP = "CategorySub";
const CATEGORY_GROUPS = [ROOT_GROUP, SUB_GROUP];
const regexPreviewSamples = [
  "电视剧",
  "连续剧",
  "国产剧",
  "日本剧",
  "日剧",
  "国漫",
  "国产动漫",
  "日韩综艺",
  "体育赛事",
  "篮球",
];

const groupLabelMap: Record<string, string> = {
  [ROOT_GROUP]: "一级分类规则",
  [SUB_GROUP]: "二级分类规则",
};

const groupHelpMap: Record<string, string[]> = {
  [ROOT_GROUP]: [
    "CategoryRoot = 前台一级大类，决定电影、剧集、动漫等主导航落点。",
    "一级分类规则应该收敛成少量稳定的大类，避免把来源站的细碎分类直接暴露到前台。",
  ],
  [SUB_GROUP]: [
    "CategorySub = filmClassifySearch > 类型，决定大类页内的类型筛选，如国产剧、日剧、动作片。",
    "这里处理的是二级类型，不是前台 filmClassifySearch > 类别。",
  ],
};

function resolveGroupLabel(group: string) {
  return groupLabelMap[group] || group;
}

function resolveGroupHelp(group: string) {
  return groupHelpMap[group] || [];
}

function normalizeRuleRecord(record: Record<string, unknown>): MappingRuleRecord {
  return {
    id: Number(record.id ?? record.ID ?? 0),
    group: String(record.group ?? record.Group ?? ""),
    raw: String(record.raw ?? record.Raw ?? ""),
    target: String(record.target ?? record.Target ?? ""),
    matchType: String(record.matchType ?? record.MatchType ?? "exact"),
    remarks: String(record.remarks ?? record.Remarks ?? ""),
  };
}

function normalizeTree(nodes: FilmClassNode[], parentId = 0): FilmClassNode[] {
  return nodes.map((node, index) => ({
    ...node,
    pid: parentId,
    sort: index + 1,
    children: normalizeTree(node.children || [], node.id),
  }));
}

function cloneTree(nodes: FilmClassNode[]): FilmClassNode[] {
  return nodes.map((node) => ({
    ...node,
    children: cloneTree(node.children || []),
  }));
}

function flattenTree(nodes: FilmClassNode[]): FilmClassNode[] {
  return nodes.flatMap((node) => [node, ...flattenTree(node.children || [])]);
}

function collectStats(nodes: FilmClassNode[]) {
  const flat = flattenTree(nodes);
  return {
    total: flat.length,
    roots: nodes.length,
    children: flat.filter((node) => node.pid > 0).length,
    hidden: flat.filter((node) => !node.show).length,
  };
}

function serializeTree(nodes: FilmClassNode[]) {
  return nodes.map((node) => ({
    id: node.id,
    name: node.name,
    children: serializeTree(node.children || []),
  }));
}

function buildNodeKey(id: number) {
  return `node-${id}`;
}

function parseNodeKey(key: React.Key) {
  return Number(String(key).replace("node-", "") || 0);
}

function parseRuleList(resp: Record<string, any>, current: number, pageSize: number) {
  const data = resp?.data || {};
  const list = Array.isArray(data.list)
    ? data.list
    : Array.isArray(data.records)
      ? data.records
      : Array.isArray(data.items)
        ? data.items
        : [];
  return {
    rules: list.map((item: Record<string, unknown>) => normalizeRuleRecord(item)),
    paging: {
      current: Number(data.current ?? data.page ?? current),
      pageSize: Number(data.pageSize ?? data.size ?? pageSize),
      total: Number(data.total ?? list.length),
    } satisfies PagingState,
  };
}

function reorderList<T>(items: T[], fromIndex: number, toIndex: number) {
  const next = items.slice();
  const [moved] = next.splice(fromIndex, 1);
  next.splice(toIndex, 0, moved);
  return next;
}

function buildTreeData(nodes: FilmClassNode[]): ClassTreeDataNode[] {
  return nodes.map((node) => ({
    key: buildNodeKey(node.id),
    title: node.name,
    rawNode: node,
    children: buildTreeData(node.children || []),
  }));
}

export default function CategoryWorkspacePageView() {
  const { message } = useAppMessage();
  const [classTree, setClassTree] = useState<FilmClassNode[]>([]);
  const [originalTree, setOriginalTree] = useState<FilmClassNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);
  const [loadingTree, setLoadingTree] = useState(false);
  const [savingTree, setSavingTree] = useState(false);
  const [resettingTree, setResettingTree] = useState(false);
  const [resetConfirmOpen, setResetConfirmOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editingItem, setEditingItem] = useState<FilmClassNode | null>(null);
  const [editForm] = Form.useForm<{ name: string; show: boolean }>();

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
  const selectedGroupForHelp = watchedGroup || ruleGroup || ROOT_GROUP;
  const treeData = useMemo(() => buildTreeData(classTree), [classTree]);
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
          const list = Array.isArray(resp.data?.rules)
            ? resp.data.rules
            : Array.isArray(resp.data)
              ? resp.data
              : [];
          setConflictRules(list.map((item: Record<string, unknown>) => normalizeRuleRecord(item)));
        }
      } finally {
        setCheckingConflict(false);
      }
    }, 250);
    return () => window.clearTimeout(timer);
  }, [editorVisible, editingRule?.id, watchedGroup, watchedRaw, watchedMatchType]);

  const changeClassState = async (id: number, show: boolean) => {
    const resp = await ApiPost("/manage/film/class/update", { id, show });
    if (resp.code !== 0) {
      message.error(resp.msg || "更新分类状态失败");
      return;
    }
    message.success(resp.msg || "分类状态已更新");
    await fetchFilmClassTree();
  };

  const deleteClass = async (id: number) => {
    const resp = await ApiPost("/manage/film/class/del", { id: String(id) });
    if (resp.code !== 0) {
      message.error(resp.msg || "删除分类失败");
      return;
    }
    message.success(resp.msg || "分类已删除");
    await fetchFilmClassTree();
  };

  const openEditDialog = async (id: number) => {
    const resp = await ApiGet("/manage/film/class/find", { id });
    if (resp.code !== 0) {
      message.error(resp.msg || "获取分类信息失败");
      return;
    }
    const node = resp.data as FilmClassNode;
    setEditingItem(node);
    editForm.setFieldsValue({ name: node.name, show: node.show });
    setEditOpen(true);
  };

  const handleEditSubmit = async () => {
    if (!editingItem) {
      return;
    }
    const values = await editForm.validateFields();
    const resp = await ApiPost("/manage/film/class/update", {
      id: editingItem.id,
      name: values.name,
      show: values.show,
    });
    if (resp.code !== 0) {
      message.error(resp.msg || "更新分类失败");
      return;
    }
    message.success(resp.msg || "分类信息已更新");
    setEditOpen(false);
    setEditingItem(null);
    await fetchFilmClassTree();
  };

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
        message.error(resp.msg || "保存分类排序失败");
        return;
      }
      message.success(resp.msg || "分类排序已保存");
      await fetchFilmClassTree();
    } finally {
      setSavingTree(false);
    }
  };

  const handleTreeDrop: TreeProps<ClassTreeDataNode>["onDrop"] = (info) => {
    if (!info.dropToGap) {
      return;
    }

    const dragNode = info.dragNode as ClassTreeDataNode;
    const dropNode = info.node as ClassTreeDataNode;
    const dragId = dragNode.rawNode.id;
    const dropId = dropNode.rawNode.id;
    const placeAfter = info.dropPosition - Number(String(info.node.pos).split("-").pop()) > 0;

    if (dragNode.rawNode.pid === 0 && dropNode.rawNode.pid === 0) {
      const fromIndex = classTree.findIndex((item) => item.id === dragId);
      const targetIndex = classTree.findIndex((item) => item.id === dropId);
      if (fromIndex < 0 || targetIndex < 0) {
        return;
      }
      let nextIndex = targetIndex + (placeAfter ? 1 : 0);
      if (fromIndex < nextIndex) {
        nextIndex -= 1;
      }
      if (fromIndex === nextIndex) {
        return;
      }
      setClassTree((prev) => normalizeTree(reorderList(prev, fromIndex, nextIndex)));
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
        const fromIndex = siblings.findIndex((item) => item.id === dragId);
        const targetIndex = siblings.findIndex((item) => item.id === dropId);
        if (fromIndex < 0 || targetIndex < 0) {
          return prev;
        }
        let nextIndex = targetIndex + (placeAfter ? 1 : 0);
        if (fromIndex < nextIndex) {
          nextIndex -= 1;
        }
        if (fromIndex === nextIndex) {
          return prev;
        }
        root.children = reorderList(siblings, fromIndex, nextIndex);
        return normalizeTree(next);
      });
    }
  };

  const allowTreeDrop: TreeProps<ClassTreeDataNode>["allowDrop"] = ({ dragNode, dropNode, dropToGap }) => {
    if (!dropToGap) {
      return false;
    }
    const dragRaw = (dragNode as ClassTreeDataNode).rawNode;
    const dropRaw = (dropNode as ClassTreeDataNode).rawNode;
    if (dragRaw.pid === 0) {
      return dropRaw.pid === 0;
    }
    return dragRaw.pid > 0 && dragRaw.pid === dropRaw.pid;
  };

  const renderTreeNode = (treeNode: ClassTreeDataNode) => {
    const node = treeNode.rawNode;
    const isRoot = node.pid === 0;
    const childCount = node.children?.length || 0;
    const expanded = expandedKeys.includes(treeNode.key);

    return (
      <div className={`${styles.treeNode} ${isRoot ? styles.treeNodeRoot : styles.treeNodeChild}`}>
        <div className={styles.treeNodeMain}>
          <span className={styles.dragHandle}>
            <HolderOutlined />
          </span>
          <div className={styles.treeNodeTitleBlock}>
            <div className={styles.treeNodeTitleRow}>
              <span className={`${styles.levelBadge} ${isRoot ? styles.levelRoot : styles.levelChild}`}>
                {isRoot ? "一级分类" : "二级分类"}
              </span>
              <span className={styles.treeNodeTitle}>{node.name}</span>
              {!node.show && <Tag color="default">已隐藏</Tag>}
              {isRoot && childCount > 0 && <Tag color="processing">{expanded ? `已展开 ${childCount}` : `已折叠 ${childCount}`}</Tag>}
            </div>
            <div className={styles.treeNodeMeta}>
              <span>ID {node.id}</span>
              <span>排序 {node.sort || 0}</span>
              {isRoot ? <span>子分类 {childCount}</span> : <span>父级 {node.pid}</span>}
            </div>
          </div>
        </div>
        <div className={styles.treeNodeActions} onClick={(event) => event.stopPropagation()}>
          <Switch
            size="small"
            checked={node.show}
            onChange={(checked) => void changeClassState(node.id, checked)}
            checkedChildren={<EyeOutlined />}
            unCheckedChildren={<EyeInvisibleOutlined />}
          />
          <Button size="small" icon={<EditOutlined />} onClick={() => void openEditDialog(node.id)}>
            编辑
          </Button>
          <Popconfirm title="确认删除该分类？" okText="删除" cancelText="取消" onConfirm={() => void deleteClass(node.id)}>
            <Button size="small" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </div>
      </div>
    );
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

  const applyEditorValues = () => {
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
      await Promise.all([fetchRules(paging.current, paging.pageSize, keyword, ruleGroup), fetchRuleTotals()]);
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
    await Promise.all([fetchRules(nextPage, paging.pageSize, keyword, ruleGroup), fetchRuleTotals()]);
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
      render: (value: string) => <strong>{value}</strong>,
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
        value ? <Tag color="processing">{value}</Tag> : <span className={styles.targetMuted}>未设置</span>,
    },
    {
      title: "说明",
      dataIndex: "remarks",
      render: (value: string) => value || <span className={styles.targetMuted}>暂无说明</span>,
    },
    {
      title: "操作",
      key: "action",
      width: 140,
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
    <ManagePageShell
      eyebrow="内容管理"
      title="分类工作台"
      description="左侧维护业务分类树，右侧集中维护一级/二级分类映射规则，适合边看分类结果边调整规则。"
      extra={
        <div className={styles.statsGrid}>
          <div className={styles.statCard}>
            <div className={styles.statLabel}>分类总数</div>
            <div className={styles.statValue}>{stats.total}</div>
          </div>
          <div className={styles.statCard}>
            <div className={styles.statLabel}>一级分类 / 二级分类</div>
            <div className={styles.statValue}>
              {stats.roots} / {stats.children}
            </div>
          </div>
          <div className={styles.statCard}>
            <div className={styles.statLabel}>隐藏分类</div>
            <div className={styles.statValue}>{stats.hidden}</div>
          </div>
          <div className={styles.statCard}>
            <div className={styles.statLabel}>一级规则 / 二级规则</div>
            <div className={styles.statValue}>
              {ruleTotals[ROOT_GROUP] || 0} / {ruleTotals[SUB_GROUP] || 0}
            </div>
          </div>
        </div>
      }
      panelless
    >
      <div className={styles.workspace}>
        <div className={styles.panelGrid}>
          <section className={styles.panel}>
            <div className={styles.panelHeader}>
              <div className={styles.panelTitleBlock}>
                <h3 className={styles.panelTitle}>业务分类树</h3>
                <div className={styles.panelDescription}>
                  用 Ant Design Tree 承载拖拽排序、展开折叠、显示隐藏、编辑删除、重置与保存结构闭环。
                </div>
              </div>
              <div className={styles.panelActions}>
                <Button icon={<ReloadOutlined />} onClick={() => void fetchFilmClassTree()} loading={loadingTree}>
                  刷新分类
                </Button>
                <Button onClick={() => setResetConfirmOpen(true)} loading={resettingTree}>
                  重置分类
                </Button>
                <Button
                  type="primary"
                  icon={<SaveOutlined />}
                  onClick={() => void saveTree()}
                  loading={savingTree}
                  disabled={!hasPendingChanges}
                >
                  保存排序
                </Button>
              </div>
            </div>
            <div className={styles.panelBody}>
              {classTree.length === 0 ? (
                <Empty description="暂无分类数据" />
              ) : (
                <div className={styles.treeWrap}>
                  <Tree<ClassTreeDataNode>
                    blockNode
                    draggable={{ icon: false }}
                    showLine={{ showLeafIcon: false }}
                    selectable={false}
                    expandedKeys={expandedKeys}
                    treeData={treeData}
                    allowDrop={allowTreeDrop}
                    onExpand={(keys) => setExpandedKeys(keys)}
                    onDrop={handleTreeDrop}
                    titleRender={renderTreeNode}
                    className={styles.tree}
                  />
                </div>
              )}
            </div>
          </section>

          <section className={styles.panel}>
            <div className={styles.panelHeader}>
              <div className={styles.panelTitleBlock}>
                <h3 className={styles.panelTitle}>分类规则面板</h3>
                <div className={styles.panelDescription}>仅管理 `CategoryRoot` 与 `CategorySub`，避免和其它映射规则混在一起。</div>
              </div>
              <div className={styles.panelActions}>
                <Button type="primary" icon={<PlusOutlined />} onClick={openCreateModal}>
                  新增规则
                </Button>
              </div>
            </div>
            <div className={styles.panelBody}>
              <div className={styles.rulesGuide}>
                <Alert
                  type="info"
                  showIcon
                  message="规则提示与前端对应"
                  description={
                    <div>
                      {resolveGroupHelp(selectedGroupForHelp).map((item) => (
                        <div key={item}>{item}</div>
                      ))}
                      <div>类别 = `filmClassifySearch &gt; 类别`，不是分类规则的分组。</div>
                    </div>
                  }
                />
              </div>
              <div className={styles.rulesToolbar}>
                <Select
                  value={ruleGroup}
                  options={CATEGORY_GROUPS.map((group) => ({ value: group, label: resolveGroupLabel(group) }))}
                  onChange={(value) => setRuleGroup(value)}
                  style={{ minWidth: 160 }}
                />
                <Input.Search
                  allowClear
                  placeholder="搜索原始值、目标值或说明"
                  value={keyword}
                  onChange={(event) => setKeyword(event.target.value)}
                  onSearch={(value) => void fetchRules(1, paging.pageSize, value, ruleGroup)}
                  style={{ flex: 1, minWidth: 220 }}
                />
                <Button icon={<ReloadOutlined />} onClick={() => void Promise.all([fetchRules(1, paging.pageSize, keyword, ruleGroup), fetchRuleTotals()])}>
                  刷新规则
                </Button>
              </div>
              <Table<MappingRuleRecord>
                rowKey="id"
                columns={ruleColumns}
                dataSource={rules}
                loading={rulesLoading}
                style={{ marginTop: 16 }}
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
          </section>
        </div>
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

      <Modal
        title="更新分类信息"
        open={editOpen}
        okText="保存"
        cancelText="取消"
        onOk={() => void handleEditSubmit()}
        onCancel={() => {
          setEditOpen(false);
          setEditingItem(null);
        }}
      >
        <Form form={editForm} layout="vertical">
          <Form.Item name="name" label="分类名称" rules={[{ required: true, message: "请输入分类名称" }]}>
            <Input placeholder="请输入分类名称" />
          </Form.Item>
          <Form.Item label="分类层级">
            {editingItem?.pid ? <Tag color="blue">二级分类</Tag> : <Tag color="gold">一级分类</Tag>}
          </Form.Item>
          <Form.Item name="show" label="展示状态" valuePropName="checked">
            <Switch checkedChildren="展示" unCheckedChildren="隐藏" />
          </Form.Item>
          {!!editingItem?.children?.length && (
            <Form.Item label="当前子分类">
              <Space wrap>
                {editingItem.children.map((child) => (
                  <Tag key={child.id}>{child.name}</Tag>
                ))}
              </Space>
            </Form.Item>
          )}
        </Form>
      </Modal>

      <Modal
        title={editingRule ? "编辑分类规则" : "新增分类规则"}
        open={editorVisible}
        width={720}
        okText="保存规则"
        cancelText="取消"
        confirmLoading={rulesSubmitting}
        afterOpenChange={(open) => {
          if (open) {
            applyEditorValues();
          }
        }}
        onOk={() => void handleRuleSubmit()}
        onCancel={closeRuleEditor}
      >
        <Form form={ruleForm} layout="vertical" initialValues={{ group: ROOT_GROUP, matchType: "exact" }}>
          <Card size="small" title="基础配置" style={{ marginBottom: 16 }}>
            <Form.Item name="group" label="规则分组" rules={[{ required: true, message: "请选择规则分组" }]}>
              <Select options={CATEGORY_GROUPS.map((group) => ({ value: group, label: resolveGroupLabel(group) }))} />
            </Form.Item>
            <Form.Item name="matchType" label="匹配方式" rules={[{ required: true, message: "请选择匹配方式" }]}>
              <Select
                options={[
                  { value: "exact", label: "精确匹配" },
                  { value: "regex", label: "正则匹配" },
                ]}
              />
            </Form.Item>
            <Form.Item name="raw" label="原始值" rules={[{ required: true, message: "请输入原始值" }]}>
              <Input placeholder="精确示例：电视剧；正则示例：^(国|国产).*(漫|动漫)$" />
            </Form.Item>
            <Form.Item name="target" label="目标值" rules={[{ required: true, message: "请输入目标值" }]}>
              <Input placeholder="如：剧集、动漫、国产剧、日剧、动作片" />
            </Form.Item>
          </Card>

          <Card size="small" title="规则提示与前端对应" style={{ marginBottom: 16 }}>
            <Space direction="vertical" size={8}>
              {resolveGroupHelp(selectedGroupForHelp).map((item) => (
                <div key={item}>{item}</div>
              ))}
              <div>类别 = `filmClassifySearch &gt; 类别`，不是分类规则分组。</div>
            </Space>
          </Card>

          {conflictRules.length > 0 && (
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 16 }}
              message="发现冲突规则"
              description={
                <Space direction="vertical" size={4}>
                  {conflictRules.map((item) => (
                    <div key={item.id}>
                      #{item.id} {item.group}/{item.raw} · 目标值：{item.target || "未设置"} · 匹配方式：
                      {item.matchType === "regex" ? "正则" : "精确"}
                    </div>
                  ))}
                </Space>
              }
            />
          )}
          {checkingConflict && conflictRules.length === 0 && (
            <Alert style={{ marginBottom: 16 }} type="info" showIcon message="正在检查冲突..." />
          )}

          <Alert
            style={{ marginBottom: 16 }}
            type={watchedMatchType === "regex" ? "warning" : "info"}
            showIcon
            message={
              watchedMatchType === "regex"
                ? "建议从 ^ 开头、$ 结尾收紧范围，避免一条规则误吞过多分类。分类规则会联动原始分类与业务分类重建。"
                : "精确匹配只会命中完全相同的原始分类名，优先级高于正则规则。"
            }
          />

          {watchedMatchType === "regex" && (
            <Card size="small" title="正则命中预览" style={{ marginBottom: 16 }}>
              {!regexPreview.valid ? (
                <Alert type="error" showIcon message={`正则无效：${regexPreview.error}`} />
              ) : (
                <Space direction="vertical" size={12} style={{ width: "100%" }}>
                  <div className={styles.previewTags}>
                    {regexPreviewSamples.map((sample) => {
                      const matched = regexPreview.matches.includes(sample);
                      return (
                        <Tag key={sample} color={matched ? "purple" : "default"}>
                          {sample}
                        </Tag>
                      );
                    })}
                  </div>
                  <Alert
                    type={regexPreview.matches.length > 0 ? "success" : "warning"}
                    showIcon
                    message={
                      regexPreview.matches.length > 0
                        ? `当前正则命中 ${regexPreview.matches.length} 个示例分类。`
                        : "当前正则未命中任何示例，请检查范围是否过窄。"
                    }
                  />
                </Space>
              )}
            </Card>
          )}

          <Form.Item name="remarks" label="补充说明">
            <Input.TextArea rows={4} placeholder="说明这条规则的用途或归一依据" />
          </Form.Item>
        </Form>
      </Modal>
    </ManagePageShell>
  );
}
