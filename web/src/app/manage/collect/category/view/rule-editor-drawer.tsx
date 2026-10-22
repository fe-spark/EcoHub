import { Alert, Button, Card, Drawer, Flex, Form, Input, Select, Space, Tag } from "antd";
import type { FormInstance } from "antd/es/form";
import type { ConflictCheckResult, MappingRuleRecord, RuleFormValues } from "./types";
import { CATEGORY_GROUPS, ROOT_GROUP, regexPreviewSamples, resolveGroupLabel } from "./types";
import styles from "./index.module.less";

interface RuleEditorDrawerProps {
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

export default function RuleEditorDrawer(props: RuleEditorDrawerProps) {
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
    <Drawer
      title={editingRule ? "编辑分类规则" : "新增分类规则"}
      open={open}
      width={600}
      onClose={onCancel}
      afterOpenChange={onAfterOpenChange}
      destroyOnClose
      extra={
        <Space>
          <Button onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button type="primary" onClick={onSubmit} loading={loading}>
            保存
          </Button>
        </Space>
      }
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
            <Input placeholder="输入原始值，如：电视剧 或 ^(国|国产).*(漫|动漫)$" />
          </Form.Item>
          <Form.Item name="target" label="目标值" rules={[{ required: true, message: "请输入目标值" }]}>
            <Input placeholder="输入目标分类，如：剧集、动漫、国产剧、日剧、动作片" />
          </Form.Item>
        </Card>

        {conflictRules.length > 0 ? (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            title="规则冲突"
            description={
              <Space direction="vertical" size={4}>
                {conflictRules.map((item) => (
                  <div key={item.id}>
                    #{item.id} {item.group}/{item.raw} · 目标值：{item.target || "-"} · {item.matchType === "regex" ? "正则" : "精确"}
                  </div>
                ))}
              </Space>
            }
          />
        ) : null}

        {checkingConflict && conflictRules.length === 0 ? (
          <Alert style={{ marginBottom: 16 }} type="info" showIcon title="正在检查冲突..." />
        ) : null}

        {watchedMatchType === "regex" ? (
          <Card size="small" title="正则命中预览" style={{ marginBottom: 16 }}>
            {!regexPreview.valid ? (
              <Alert type="error" showIcon title={`正则无效：${regexPreview.error}`} />
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
                  title={
                    regexPreview.matches.length > 0
                      ? `命中 ${regexPreview.matches.length} 个示例`
                      : "未命中任何示例"
                  }
                />
              </Space>
            )}
          </Card>
        ) : null}

        <Form.Item name="remarks" label="备注">
          <Input.TextArea rows={3} placeholder="备注说明（选填）" />
        </Form.Item>
      </Form>
    </Drawer>
  );
}
