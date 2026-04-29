import { Alert, Card, Form, Input, Modal, Select, Space, Tag } from "antd";
import type { FormInstance } from "antd/es/form";
import type { ConflictCheckResult, MappingRuleRecord, RuleFormValues } from "./types";
import { CATEGORY_GROUPS, ROOT_GROUP, regexPreviewSamples, resolveGroupLabel } from "./types";
import styles from "./index.module.less";

interface RuleEditorModalProps {
  open: boolean;
  loading: boolean;
  editingRule: MappingRuleRecord | null;
  form: FormInstance<RuleFormValues>;
  conflictRules: ConflictCheckResult[];
  checkingConflict: boolean;
  watchedMatchType?: string;
  regexPreview: {
    valid: boolean;
    matches: string[];
    error: string;
  };
  onSubmit: () => void;
  onCancel: () => void;
  onAfterOpenChange: (open: boolean) => void;
}

export default function RuleEditorModal(props: RuleEditorModalProps) {
  const {
    open,
    loading,
    editingRule,
    form,
    conflictRules,
    checkingConflict,
    watchedMatchType,
    regexPreview,
    onSubmit,
    onCancel,
    onAfterOpenChange,
  } = props;

  return (
    <Modal
      title={editingRule ? "编辑分类规则" : "新增分类规则"}
      open={open}
      width={720}
      okText="保存规则"
      cancelText="取消"
      confirmLoading={loading}
      afterOpenChange={onAfterOpenChange}
      onOk={onSubmit}
      onCancel={onCancel}
    >
      <Form form={form} layout="vertical" initialValues={{ group: ROOT_GROUP, matchType: "exact" }}>
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

        {conflictRules.length > 0 ? (
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
        ) : null}
        {checkingConflict && conflictRules.length === 0 ? (
          <Alert style={{ marginBottom: 16 }} type="info" showIcon message="正在检查冲突..." />
        ) : null}

        <Alert
          style={{ marginBottom: 16 }}
          type={watchedMatchType === "regex" ? "warning" : "info"}
          showIcon
          message={
            watchedMatchType === "regex"
              ? "建议从 ^ 开头、$ 结尾收紧范围，避免一条规则误吞过多分类。分类规则会刷新展示分类与来源映射，不回写历史影片。"
              : "精确匹配只会命中完全相同的原始分类名，优先级高于正则规则。"
          }
        />

        {watchedMatchType === "regex" ? (
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
        ) : null}

        <Form.Item name="remarks" label="补充说明">
          <Input.TextArea rows={4} placeholder="说明这条规则的用途或归一依据" />
        </Form.Item>
      </Form>
    </Modal>
  );
}
