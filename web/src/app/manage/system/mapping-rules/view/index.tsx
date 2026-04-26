"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Tooltip,
  Col,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  TagsOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageShell from "../../../components/page-shell";
import styles from "./index.module.less";

type MappingRuleRecord = {
  id: number;
  group: string;
  raw: string;
  target?: string;
  matchType: "exact" | "regex";
  remarks?: string;
  createdAt?: string;
  updatedAt?: string;
};

type MappingRuleApiRecord = {
  id?: number;
  group?: string;
  raw?: string;
  target?: string;
  matchType?: "exact" | "regex";
  remarks?: string;
  createdAt?: string;
  updatedAt?: string;
  ID?: number;
  Group?: string;
  Raw?: string;
  Target?: string;
  MatchType?: "exact" | "regex";
  Remarks?: string;
  CreatedAt?: string;
  UpdatedAt?: string;
};

type Paging = {
  current: number;
  pageSize: number;
  total: number;
  pageCount: number;
};

type MappingRuleListResult = {
  list: MappingRuleApiRecord[];
  paging: Paging;
};

type ConflictCheckResult = {
  hasConflict: boolean;
  rules: MappingRuleApiRecord[];
};

type RuleFormValues = {
  group: string;
  raw: string;
  target?: string;
  matchType: "exact" | "regex";
  remarks?: string;
};

const ROOT_GROUP = "CategoryRoot";
const SUB_GROUP = "CategorySub";
const FILTER_GROUP = "Filter";
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
  Area: "地区",
  Language: "语言",
  Filter: "过滤词",
  Attribute: "属性",
  Plot: "剧情",
  CategoryRoot: "一级分类（前端大类）",
  CategorySub: "二级分类（前端类型）",
};

const groupHelpMap: Record<string, string> = {
  Area: "对应前台筛选中的地区。",
  Language: "对应前台筛选中的语言。",
  Filter: "用于过滤无意义标签，不直接显示为前台筛选项。",
  Attribute: "用于属性归一和识别，不直接对应单独前台筛选项。",
  Plot: "对应前台筛选中的剧情。",
  CategoryRoot:
    "决定前台一级大类，例如电影、剧集、动漫。会影响 filmClassify 和 filmClassifySearch 的一级分类入口。",
  CategorySub:
    "决定 filmClassifySearch 里的类型，用来统一一级大类下面的二级分类，例如国产剧、日剧、动作片。",
};

function resolveGroupLabel(group: string) {
  return groupLabelMap[group] || group;
}

function resolveGroupHelp(group: string) {
  return groupHelpMap[group] || "该分组用于规则归一。";
}

function normalizeRuleRecord(record: MappingRuleApiRecord): MappingRuleRecord {
  return {
    id: record.id ?? record.ID ?? 0,
    group: record.group ?? record.Group ?? "",
    raw: record.raw ?? record.Raw ?? "",
    target: record.target ?? record.Target ?? "",
    matchType: (record.matchType ?? record.MatchType ?? "exact") as
      | "exact"
      | "regex",
    remarks: record.remarks ?? record.Remarks ?? "",
    createdAt: record.createdAt ?? record.CreatedAt,
    updatedAt: record.updatedAt ?? record.UpdatedAt,
  };
}

export default function MappingRulesPageView() {
  const { message } = useAppMessage();
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [groups, setGroups] = useState<string[]>([]);
  const [rules, setRules] = useState<MappingRuleRecord[]>([]);
  const [paging, setPaging] = useState<Paging>({
    current: 1,
    pageSize: 10,
    total: 0,
    pageCount: 1,
  });
  const [groupFilter, setGroupFilter] = useState<string>("");
  const [keyword, setKeyword] = useState("");
  const [editingRule, setEditingRule] = useState<MappingRuleRecord | null>(
    null,
  );
  const [editorVisible, setEditorVisible] = useState(false);
  const [checkingConflict, setCheckingConflict] = useState(false);
  const [conflictRules, setConflictRules] = useState<MappingRuleRecord[]>([]);
  const [form] = Form.useForm<RuleFormValues>();
  const watchedRaw = Form.useWatch("raw", form);
  const watchedMatchType = Form.useWatch("matchType", form);
  const watchedGroup = Form.useWatch("group", form);
  const isFilterGroup = watchedGroup === FILTER_GROUP;

  const applyEditorValues = useCallback(() => {
    if (editingRule) {
      form.setFieldsValue({
        group: editingRule.group,
        raw: editingRule.raw,
        target: editingRule.target,
        matchType: editingRule.matchType,
        remarks: editingRule.remarks,
      });
      return;
    }

    form.setFieldsValue({
      group: groupFilter || ROOT_GROUP,
      raw: "",
      target: "",
      matchType: "exact",
      remarks: "",
    });
  }, [editingRule, form, groupFilter]);

  const fetchGroups = useCallback(async () => {
    const resp = await ApiGet<string[]>("/manage/mapping/group/list");
    if (resp.code === 0) {
      setGroups(resp.data || []);
      return;
    }
    message.error(resp.msg || "映射规则分组获取失败");
  }, [message]);

  const fetchRules = useCallback(
    async (
      page = paging.current,
      pageSize = paging.pageSize,
      nextGroup = groupFilter,
      nextKeyword = keyword,
    ) => {
      setLoading(true);
      try {
        const resp = await ApiGet<MappingRuleListResult>(
          "/manage/mapping/rule/list",
          {
            current: page,
            pageSize,
            group: nextGroup || undefined,
            keyword: nextKeyword || undefined,
          },
        );
        if (resp.code === 0) {
          const normalizedList = (resp.data?.list || []).map(
            normalizeRuleRecord,
          );
          setRules(normalizedList);
          setPaging(
            resp.data?.paging || {
              current: page,
              pageSize,
              total: 0,
              pageCount: 1,
            },
          );
          return;
        }
        message.error(resp.msg || "映射规则获取失败");
      } finally {
        setLoading(false);
      }
    },
    [groupFilter, keyword, message, paging.current, paging.pageSize],
  );

  useEffect(() => {
    fetchGroups();
  }, [fetchGroups]);

  useEffect(() => {
    fetchRules(1, paging.pageSize, groupFilter, keyword);
  }, [fetchRules, groupFilter, keyword, paging.pageSize]);

  useEffect(() => {
    if (!isFilterGroup) {
      return;
    }
    form.setFieldValue("target", "");
  }, [form, isFilterGroup]);

  useEffect(() => {
    if (!editorVisible) {
      setConflictRules([]);
      setCheckingConflict(false);
      return;
    }

    const group = (watchedGroup || "").trim();
    const raw = (watchedRaw || "").trim();
    const matchType = watchedMatchType || "exact";
    if (!group || !raw) {
      setConflictRules([]);
      setCheckingConflict(false);
      return;
    }

    const timer = window.setTimeout(async () => {
      setCheckingConflict(true);
      try {
        const resp = await ApiPost<ConflictCheckResult>(
          "/manage/mapping/rule/check",
          {
            id: editingRule?.id,
            group,
            raw,
            matchType,
          },
        );
        if (resp.code === 0) {
          setConflictRules((resp.data?.rules || []).map(normalizeRuleRecord));
          return;
        }
        setConflictRules([]);
      } finally {
        setCheckingConflict(false);
      }
    }, 250);

    return () => {
      window.clearTimeout(timer);
    };
  }, [
    editorVisible,
    watchedGroup,
    watchedRaw,
    watchedMatchType,
    editingRule?.id,
  ]);

  const rootRuleCount = useMemo(
    () => rules.filter((item) => item.group === ROOT_GROUP).length,
    [rules],
  );
  const subRuleCount = useMemo(
    () => rules.filter((item) => item.group === SUB_GROUP).length,
    [rules],
  );
  const selectedGroupForHelp = watchedGroup || groupFilter || ROOT_GROUP;

  const openCreateModal = () => {
    setEditingRule(null);
    form.resetFields();
    setEditorVisible(true);
  };

  const openEditModal = (record: MappingRuleRecord) => {
    setEditingRule(record);
    setEditorVisible(true);
  };

  const closeEditor = () => {
    setEditorVisible(false);
    setEditingRule(null);
    form.resetFields();
  };

  const regexPreview = useMemo(() => {
    if (watchedMatchType !== "regex") {
      return {
        valid: true,
        matches: [] as string[],
        error: "",
      };
    }

    const source = watchedRaw?.trim() || "";
    if (!source) {
      return {
        valid: false,
        matches: [] as string[],
        error: "请输入正则表达式后查看命中预览",
      };
    }

    try {
      const pattern = new RegExp(source);
      return {
        valid: true,
        matches: regexPreviewSamples.filter((item) => pattern.test(item)),
        error: "",
      };
    } catch (error) {
      return {
        valid: false,
        matches: [] as string[],
        error: error instanceof Error ? error.message : "正则表达式不合法",
      };
    }
  }, [watchedMatchType, watchedRaw]);

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      const payload = {
        ...values,
        raw: values.raw.trim(),
        target:
          values.group === FILTER_GROUP ? "" : values.target?.trim() || "",
        matchType: values.matchType,
        remarks: values.remarks?.trim() || "",
        ...(editingRule ? { id: editingRule.id } : {}),
      };
      const resp = await ApiPost(
        editingRule
          ? "/manage/mapping/rule/update"
          : "/manage/mapping/rule/add",
        payload,
      );
      if (resp.code === 0) {
        message.success(
          resp.msg || (editingRule ? "规则更新成功" : "规则创建成功"),
        );
        closeEditor();
        fetchRules(editingRule ? paging.current : 1, paging.pageSize);
      } else {
        message.error(resp.msg || "映射规则保存失败");
      }
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (id: number) => {
    const resp = await ApiPost("/manage/mapping/rule/del", { id });
    if (resp.code === 0) {
      message.success(resp.msg || "规则删除成功");
      const nextPage =
        rules.length === 1 && paging.current > 1
          ? paging.current - 1
          : paging.current;
      fetchRules(nextPage, paging.pageSize);
      return;
    }
    message.error(resp.msg || "规则删除失败");
  };

  const columns: ColumnsType<MappingRuleRecord> = [
    {
      title: "分组",
      dataIndex: "group",
      key: "group",
      width: 140,
      render: (group: string) => {
        const color =
          group === ROOT_GROUP
            ? "gold"
            : group === SUB_GROUP
              ? "blue"
              : "default";
        return <Tag color={color}>{resolveGroupLabel(group)}</Tag>;
      },
    },
    {
      title: "原始值",
      dataIndex: "raw",
      key: "raw",
      render: (value: string) => (
        <span style={{ fontWeight: 600 }}>{value}</span>
      ),
    },
    {
      title: "匹配方式",
      dataIndex: "matchType",
      key: "matchType",
      width: 120,
      render: (value: MappingRuleRecord["matchType"]) => (
        <Tag color={value === "regex" ? "purple" : "default"}>
          {value === "regex" ? "正则" : "精确"}
        </Tag>
      ),
    },
    {
      title: "目标值",
      dataIndex: "target",
      key: "target",
      render: (_value: string | undefined, record) => {
        const currentValue = record.target?.trim() || "";

        return (
          <div className={styles.targetCell}>
            {currentValue ? (
              <Tag color="processing">{currentValue}</Tag>
            ) : (
              <span className={styles.targetMuted}>空值</span>
            )}
          </div>
        );
      },
    },
    {
      title: "备注",
      dataIndex: "remarks",
      key: "remarks",
      ellipsis: true,
      render: (value?: string) =>
        value || <span className={styles.targetMuted}>未填写</span>,
    },
    {
      title: "操作",
      key: "action",
      width: 120,
      render: (_value, record) => (
        <Space size={8}>
          <Tooltip title="编辑规则">
            <Button
              type="primary"
              shape="circle"
              size="small"
              icon={<EditOutlined />}
              onClick={() => openEditModal(record)}
            />
          </Tooltip>
          <Popconfirm
            title="确定删除这条映射规则吗？"
            okText="确定"
            cancelText="取消"
            onConfirm={() => handleDelete(record.id)}
          >
            <Tooltip title="删除规则">
              <Button
                danger
                shape="circle"
                size="small"
                icon={<DeleteOutlined />}
              />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <ManagePageShell
      eyebrow="系统设置"
      title="映射规则"
      description="集中维护分类、地区、语言等标准化映射。当前页重点覆盖 CategoryRoot 与 CategorySub，也兼容其他映射组统一管理。"
      actions={
        <div className={styles.toolbar}>
          <Select
            allowClear
            placeholder="按分组筛选"
            style={{ width: 180 }}
            value={groupFilter || undefined}
            onChange={(value) => {
              setGroupFilter(value || "");
              setPaging((prev) => ({ ...prev, current: 1 }));
            }}
            options={groups.map((group) => ({
              value: group,
              label: resolveGroupLabel(group),
            }))}
          />
          <Input.Search
            allowClear
            placeholder="搜索原始值、目标值或备注"
            style={{ width: 280 }}
            onSearch={(value) => {
              setKeyword(value.trim());
              setPaging((prev) => ({ ...prev, current: 1 }));
            }}
          />
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={openCreateModal}
          >
            新增规则
          </Button>
        </div>
      }
      extra={
        <div className={styles.stats}>
          <Card className={styles.statCard}>
            <div className={styles.statLabel}>当前结果</div>
            <div className={styles.statValue}>{paging.total}</div>
          </Card>
          <Card className={styles.statCard}>
            <div className={styles.statLabel}>一级分类规则</div>
            <div className={styles.statValue}>{rootRuleCount}</div>
          </Card>
          <Card className={styles.statCard}>
            <div className={styles.statLabel}>二级分类规则</div>
            <div className={styles.statValue}>{subRuleCount}</div>
          </Card>
        </div>
      }
      panelClassName={styles.panel}
      panelless
    >
      <Space direction="vertical" size={16} style={{ width: "100%" }}>
        <div className={styles.inlineGuide}>
          <div className={styles.inlineGuideTitle}>
            <Space>
              <TagsOutlined />
              推荐关注
            </Space>
          </div>
          <div className={styles.hintList}>
            <Tag className={styles.filterTag} color="gold">
              CategoryRoot: 决定前台一级大类
            </Tag>
            <Tag className={styles.filterTag} color="blue">
              CategorySub: 决定 filmClassifySearch 类型
            </Tag>
            <Tag className={styles.filterTag}>Plot: 前端剧情筛选</Tag>
            <Tag className={styles.filterTag}>
              Area / Language: 前端地区/语言
            </Tag>
            <Tag className={styles.filterTag}>
              类别: 页内拆分原始一级来源，不属于规则分组
            </Tag>
          </div>
        </div>

        <Table
          rowKey="id"
          loading={loading}
          columns={columns}
          dataSource={rules}
          scroll={{ x: 980 }}
          className={styles.customTable}
          title={() => (
            <div className={styles.tableTitleWrap}>
              <div>
                <div className={styles.tableTitle}>自定义规则</div>
                <div className={styles.tableDesc}>
                  这里只展示数据库中的实际规则，可新增、编辑、删除，并参与当前系统重建。
                </div>
              </div>
              <Tag color="processing">{paging.total} 条</Tag>
            </div>
          )}
          pagination={{
            current: paging.current,
            pageSize: paging.pageSize,
            total: paging.total,
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 条自定义规则`,
            onChange: (page, pageSize) => {
              fetchRules(page, pageSize);
            },
          }}
        />
      </Space>

      <Modal
        title={editingRule ? "编辑映射规则" : "新增映射规则"}
        open={editorVisible}
        onOk={handleSubmit}
        onCancel={closeEditor}
        confirmLoading={submitting}
        destroyOnHidden
        afterOpenChange={(open) => {
          if (open) {
            applyEditorValues();
          }
        }}
      >
        <Form
          form={form}
          layout="vertical"
          preserve={false}
          initialValues={{ group: ROOT_GROUP, matchType: "exact" }}
          className={styles.editorForm}
        >
          <div className={styles.editorSection}>
            <div className={styles.sectionTitle}>基础配置</div>
            <Row gutter={16}>
              <Col xs={24} sm={12}>
                <Form.Item
                  name="group"
                  label="规则分组"
                  rules={[{ required: true, message: "请选择规则分组" }]}
                >
                  <Select
                    options={groups.map((group) => ({
                      value: group,
                      label: resolveGroupLabel(group),
                    }))}
                  />
                </Form.Item>
              </Col>
              <Col xs={24} sm={12}>
                <Form.Item
                  name="matchType"
                  label="匹配方式"
                  rules={[{ required: true, message: "请选择匹配方式" }]}
                >
                  <Select
                    options={[
                      { value: "exact", label: "精确匹配" },
                      { value: "regex", label: "正则匹配" },
                    ]}
                  />
                </Form.Item>
              </Col>
            </Row>
            <Form.Item
              name="raw"
              label="原始值"
              rules={[{ required: true, message: "请输入原始值" }]}
            >
              <Input
                placeholder="精确示例：电视剧；正则示例：^(国|国产).*(漫|动漫)$"
                maxLength={128}
              />
            </Form.Item>
            <Form.Item name="target" label="目标值">
              <Input
                placeholder={
                  isFilterGroup
                    ? "过滤词不需要目标值，系统会直接忽略该词"
                    : "例如：剧集、国产动漫、日剧；过滤词可留空"
                }
                maxLength={128}
                disabled={isFilterGroup}
              />
            </Form.Item>
            {isFilterGroup ? (
              <div className={styles.fieldHint}>
                `Filter` 只负责过滤噪音词，不做目标值映射，保持空值即可。
              </div>
            ) : null}
          </div>

          <div className={`${styles.editorSection} ${styles.hintSection}`}>
            <div className={styles.sectionTitle}>规则提示与前端对应</div>
            <Card size="small" className={styles.mappingGuideCard}>
              <div className={styles.mappingGuideTitle}>
                当前分组影响哪里显示
              </div>
              <div className={styles.mappingGuideText}>
                {resolveGroupHelp(selectedGroupForHelp)}
              </div>
              <div className={styles.mappingGuideList}>
                <div>
                  <strong>CategoryRoot = 前台一级大类</strong>
                  <span>决定前台展示哪些一级大类，例如电影、剧集、动漫。</span>
                </div>
                <div>
                  <strong>CategorySub = filmClassifySearch &gt; 类型</strong>
                  <span>
                    决定大类页里的类型筛选，例如国产剧、日剧、动作片。
                  </span>
                </div>
                <div>
                  <strong>类别 = filmClassifySearch &gt; 类别</strong>
                  <span>
                    这是页内用来拆分原始一级来源的显示项，不是 `mapping-rules`
                    的规则分组。
                  </span>
                </div>
              </div>
            </Card>

            {conflictRules.length > 0 ? (
              <Alert
                type="warning"
                showIcon
                className={styles.conflictAlert}
                title="检测到冲突规则"
                description={
                  <div className={styles.conflictList}>
                    <div>
                      当前表单与以下规则命中了相同作用点：同分组、同匹配方式、同原始值。
                    </div>
                    {conflictRules.map((rule) => (
                      <div key={rule.id} className={styles.conflictItem}>
                        <span className={styles.conflictCode}>
                          #{rule.id} {resolveGroupLabel(rule.group)} /{" "}
                          {rule.raw}
                        </span>
                        <span>
                          目标值：{rule.target?.trim() || "空值"}，匹配方式：
                          {rule.matchType === "regex" ? "正则" : "精确"}
                        </span>
                      </div>
                    ))}
                  </div>
                }
              />
            ) : null}

            {checkingConflict && !conflictRules.length ? (
              <div className={styles.conflictChecking}>正在检查冲突...</div>
            ) : null}

            <Alert
              className={styles.regexHint}
              type={
                watchedMatchType === "regex"
                  ? regexPreview.valid
                    ? "info"
                    : "warning"
                  : "info"
              }
              showIcon
              title={
                watchedMatchType === "regex" ? "正则规则提示" : "精确规则提示"
              }
              description={
                watchedMatchType === "regex"
                  ? "建议从 ^ 开头、$ 结尾收紧范围，避免一条规则误吞过多分类。分类规则会联动原始分类与业务分类重建。"
                  : "精确匹配只会命中完全相同的原始分类名，优先级高于正则规则。"
              }
            />

            {watchedMatchType === "regex" ? (
              <Card
                size="small"
                title="正则命中预览"
                className={styles.regexPreviewCard}
              >
                <div className={styles.previewExamples}>
                  {regexPreviewSamples.map((item) => {
                    const matched = regexPreview.matches.includes(item);
                    return (
                      <Tag key={item} color={matched ? "purple" : "default"}>
                        {item}
                      </Tag>
                    );
                  })}
                </div>
                <div className={styles.previewSummary}>
                  {regexPreview.valid ? (
                    regexPreview.matches.length > 0 ? (
                      <span>
                        当前示例命中 {regexPreview.matches.length}{" "}
                        项，可用来快速检查范围是否过宽或过窄。
                      </span>
                    ) : (
                      <span>
                        当前示例未命中任何项，若这是预期可忽略，否则请检查正则写法。
                      </span>
                    )
                  ) : (
                    <span className={styles.previewError}>
                      正则无效：{regexPreview.error}
                    </span>
                  )}
                </div>
              </Card>
            ) : null}
          </div>

          <div className={styles.editorSection}>
            <div className={styles.sectionTitle}>补充说明</div>
            <Form.Item
              name="remarks"
              label="备注"
              className={styles.compactItem}
            >
              <Input.TextArea
                placeholder="说明这条规则的用途或归一依据"
                autoSize={{ minRows: 3, maxRows: 6 }}
                maxLength={256}
              />
            </Form.Item>
          </div>
        </Form>
      </Modal>
    </ManagePageShell>
  );
}
