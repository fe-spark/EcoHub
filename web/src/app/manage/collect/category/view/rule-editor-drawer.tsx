import { Alert, Button, Drawer, Empty, Form, Input, Segmented, Space, Spin, Tag, Typography } from "antd";
import type { FormInstance } from "antd/es/form";
import { FolderOpenOutlined } from "@ant-design/icons";
import type { MappingRuleRecord, RuleFormValues, RuleFullPreview } from "./types";
import { ROOT_GROUP, SUB_GROUP, resolveGroupLabel } from "./types";
import styles from "./index.module.less";

interface RuleEditorDrawerProps {
  open: boolean;
  loading: boolean;
  contextLoading: boolean;
  canWrite: boolean;
  sourceName: string;
  editingRule: MappingRuleRecord | null;
  form: FormInstance<RuleFormValues>;
  conflictRules: MappingRuleRecord[];
  watchedGroup?: string;
  preview: RuleFullPreview;
  onPickRawName: (rawName: string) => void;
  onSubmit: () => void;
  onCancel: () => void;
  onAfterOpenChange: (open: boolean) => void;
}

export default function RuleEditorDrawer(props: RuleEditorDrawerProps) {
  const {
    open,
    loading,
    contextLoading,
    canWrite,
    sourceName,
    editingRule,
    form,
    conflictRules,
    watchedGroup,
    preview,
    onPickRawName,
    onSubmit,
    onCancel,
    onAfterOpenChange,
  } = props;

  const activeGroup = watchedGroup || ROOT_GROUP;
  const canPickRoot = activeGroup === ROOT_GROUP;
  const canPickSub = activeGroup === SUB_GROUP;
  const previewTitle = preview.hasDraftRaw ? "最终全部分类预览" : "当前全部分类";

  return (
    <Drawer
      title={
        <Space size={8}>
          <span>{editingRule ? "编辑分类规则" : "新增分类规则"}</span>
          {sourceName ? <Tag style={{ margin: 0 }}>{sourceName}</Tag> : null}
        </Space>
      }
      open={open}
      size={900}
      onClose={onCancel}
      afterOpenChange={onAfterOpenChange}
      destroyOnClose
      styles={{
        body: {
          padding: 0,
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
        },
      }}
      extra={
        <Space>
          <Button onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button type="primary" onClick={onSubmit} loading={loading} disabled={!canWrite}>
            保存
          </Button>
        </Space>
      }
    >
      <div className={styles.drawerSplitLayout}>
        <div className={styles.drawerFormPane}>
          <div>
            <div className={styles.drawerPaneSectionTitle}>规则配置</div>
            <Form form={form} layout="vertical" initialValues={{ group: ROOT_GROUP, matchType: "exact" }}>
              <Form.Item name="group" label="规则分组" rules={[{ required: true, message: "请选择规则分组" }]}>
                <Segmented
                  block
                  options={[
                    { value: ROOT_GROUP, label: "一级分类" },
                    { value: SUB_GROUP, label: "二级分类" },
                  ]}
                />
              </Form.Item>
              <Form.Item name="matchType" label="匹配方式" rules={[{ required: true, message: "请选择匹配方式" }]}>
                <Segmented
                  block
                  options={[
                    { value: "exact", label: "精确匹配" },
                    { value: "regex", label: "正则匹配" },
                  ]}
                />
              </Form.Item>
              <Form.Item name="raw" label="原始值" rules={[{ required: true, message: "请输入原始值" }]}>
                <Input placeholder="输入分类名/正则，或点击右侧分类填入" allowClear />
              </Form.Item>
              <Form.Item name="target" label="目标值" rules={[{ required: true, message: "请输入目标值" }]}>
                <Input placeholder="输入合并或重命名后的目标分类名" allowClear />
              </Form.Item>
              <Form.Item name="remarks" label="备注" style={{ marginBottom: 0 }}>
                <Input.TextArea rows={2} placeholder="备注说明（选填）" />
              </Form.Item>
            </Form>
          </div>

          {conflictRules.length > 0 ? (
            <Alert
              type="warning"
              showIcon
              title="存在重复规则"
              description={
                <Space direction="vertical" size={4}>
                  {conflictRules.map((item) => (
                    <div key={item.id}>
                      #{item.id} {resolveGroupLabel(item.group)}/{item.raw} · 目标：{item.target || "-"} ·{" "}
                      {item.matchType === "regex" ? "正则" : "精确"}
                    </div>
                  ))}
                </Space>
              }
            />
          ) : null}

          {!preview.valid && preview.error ? (
            <Alert type="error" showIcon title={preview.error} />
          ) : null}
        </div>

        <div className={styles.drawerPreviewPane}>
          <div className={styles.drawerPreviewHeader}>
            <div className={styles.drawerPreviewTitle}>
              <span>{previewTitle}</span>
            </div>
            <div className={styles.drawerPreviewStats}>
              {preview.hasDraftRaw && preview.valid ? (
                <Tag color={preview.hitCount > 0 ? "processing" : "warning"} style={{ margin: 0 }}>
                  {preview.hitCount > 0 ? `命中 ${preview.hitCount} 项` : "未命中分类"}
                </Tag>
              ) : null}
              <span>
                一级 {preview.rootCount} · 二级 {preview.subCount}
              </span>
            </div>
          </div>

          {preview.hasDraftRaw && preview.valid ? (
            <div className={styles.drawerPreviewBanner}>
              {preview.hitCount > 0 ? (
                <Alert
                  type="info"
                  showIcon
                  title={
                    preview.draftTarget
                      ? `已命中 ${preview.hitCount} 个分类 → 将归入「${preview.draftTarget}」`
                      : `已命中 ${preview.hitCount} 个分类，请输入目标值查看合并效果`
                  }
                />
              ) : (
                <Alert type="warning" showIcon title="当前原始值未命中任何分类，下方仍为全部现有分类" />
              )}
            </div>
          ) : null}

          <div className={styles.drawerPreviewScroll}>
            <Spin spinning={contextLoading} description="正在拉取当前分类...">
              {preview.tree.length === 0 ? (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={contextLoading ? "加载中..." : "该采集站暂无分类数据，请先在采集站管理同步分类"}
                />
              ) : (
                <div className={styles.drawerPreviewList}>
                  {preview.tree.map((root) => {
                    const pickableRootRaw = canPickRoot && !root.synthetic ? root.rawNames[0] || root.name : "";
                    const rawDiff =
                      !root.synthetic && root.rawNames.length > 0 && root.rawNames.join("、") !== root.name
                        ? root.rawNames.join("、")
                        : "";
                    return (
                      <div
                        key={root.key}
                        className={`${styles.previewRootCard} ${
                          root.affected ? styles.previewRootCardAffected : ""
                        }`}
                      >
                        <div className={styles.previewRootHeader}>
                          <span
                            className={`${styles.previewRootName} ${
                              pickableRootRaw ? styles.previewClickable : ""
                            }`}
                            title={pickableRootRaw ? `点击填入原始值「${pickableRootRaw}」` : undefined}
                            onClick={() => {
                              if (pickableRootRaw) {
                                onPickRawName(pickableRootRaw);
                              }
                            }}
                          >
                            <FolderOpenOutlined style={{ color: "var(--ant-color-primary)", fontSize: 13 }} />
                            <span>{root.name}</span>
                            {rawDiff ? (
                              <span className={styles.previewRawHint}>(原: {rawDiff})</span>
                            ) : null}
                          </span>

                          <Space size={4}>
                            {root.children.length > 0 ? (
                              <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                                {root.children.length} 子类
                              </Typography.Text>
                            ) : null}
                            {root.synthetic ? (
                              <Tag color="purple" style={{ margin: 0 }}>
                                合成
                              </Tag>
                            ) : null}
                            {root.hitByDraft ? (
                              <Tag color="processing" style={{ margin: 0 }}>
                                命中
                              </Tag>
                            ) : null}
                            {!root.show ? (
                              <Tag color="default" style={{ margin: 0 }}>
                                隐藏
                              </Tag>
                            ) : null}
                          </Space>
                        </div>

                        {root.children.length > 0 ? (
                          <div className={styles.previewChildrenWrap}>
                            {root.children.map((child) => {
                              const mergedDiff =
                                child.rawNames.length > 1 ||
                                (child.rawNames.length === 1 && child.rawNames[0] !== child.name);
                              const rawTip = child.rawNames[0] || child.name;
                              const clickable = canPickSub || (canPickRoot && child.fromRoot);
                              return (
                                <Tag
                                  key={child.key}
                                  color={child.hitByDraft ? "processing" : child.show ? undefined : "default"}
                                  className={`${styles.previewChildTag} ${
                                    clickable ? styles.previewChildTagClickable : ""
                                  }`}
                                  title={
                                    clickable
                                      ? `点击填入原始值「${rawTip}」${mergedDiff ? `（来自：${child.rawNames.join("、")}）` : ""}`
                                      : mergedDiff
                                        ? `原始分类：${child.rawNames.join("、")}`
                                        : undefined
                                  }
                                  onClick={() => {
                                    if (clickable) {
                                      onPickRawName(rawTip);
                                    }
                                  }}
                                >
                                  <span>{child.name}</span>
                                  {mergedDiff ? (
                                    <span className={styles.previewRawHint}>
                                      ← {child.rawNames.join("、")}
                                    </span>
                                  ) : null}
                                  {child.fromRoot ? <span className={styles.previewRawHint}>· 原一级</span> : null}
                                  {!child.show ? <span className={styles.previewRawHint}>· 隐藏</span> : null}
                                </Tag>
                              );
                            })}
                          </div>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              )}
            </Spin>
          </div>

          <div className={styles.drawerPreviewFooter}>
            {canPickRoot
              ? "点击右侧「一级分类」或「原一级」项可填入原始值，正则模式支持连续点击拼接。"
              : "点击右侧「二级分类」标签可填入原始值，正则模式支持连续点击拼接。"}
          </div>
        </div>
      </div>
    </Drawer>
  );
}
