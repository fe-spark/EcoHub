import { Alert, Button, Drawer, Flex, Form, Input, Radio, Space, Switch } from "antd";
import { useEffect, useMemo } from "react";
import { useManagePermission } from "@/lib/manage-permission";
import type { SourceFormValues } from "./types";

function isDomainReplaceRuleLine(line: string): boolean {
  let from = "";
  let to = "";
  if (line.includes("=>")) {
    const i = line.indexOf("=>");
    from = line.slice(0, i);
    to = line.slice(i + 2);
  } else if (line.includes("->")) {
    const i = line.indexOf("->");
    from = line.slice(0, i);
    to = line.slice(i + 2);
  } else if (line.includes(",")) {
    const i = line.indexOf(",");
    from = line.slice(0, i);
    to = line.slice(i + 1);
  } else {
    const fields = line.trim().split(/\s+/);
    if (fields.length === 2) {
      from = fields[0];
      to = fields[1];
    }
  }
  return Boolean(from.trim() && to.trim());
}

interface SourceFormDrawerProps {
  open: boolean;
  mode: "add" | "edit";
  loading: boolean;
  testing?: boolean;
  globalSpiderProxyReady?: boolean;
  initialValues: SourceFormValues;
  formNonce: number;
  onCancel: () => void;
  onSubmit: (values: SourceFormValues) => Promise<void> | void;
  onTest: (values: SourceFormValues) => void;
}

export default function SourceFormDrawer(props: SourceFormDrawerProps) {
  const {
    open,
    mode,
    loading,
    testing,
    globalSpiderProxyReady = false,
    initialValues,
    onCancel,
    onSubmit,
    onTest,
  } = props;
  const [form] = Form.useForm<SourceFormValues>();
  const { canWrite } = useManagePermission();
  const title = useMemo(
    () => (mode === "add" ? "新增采集站" : "编辑采集站"),
    [mode],
  );

  const currentUri = Form.useWatch("uri", form);
  const isUriChanged =
    mode === "edit" &&
    Boolean(
      currentUri &&
        initialValues.uri &&
        currentUri.trim() !== initialValues.uri.trim(),
    );

  useEffect(() => {
    if (!open) {
      return;
    }
    form.resetFields();
    const effectiveValues: SourceFormValues = {
      ...initialValues,
      proxyCollect: globalSpiderProxyReady ? Boolean(initialValues.proxyCollect) : false,
    };
    form.setFieldsValue(effectiveValues);
  }, [open, form, initialValues, globalSpiderProxyReady]);

  return (
    <Drawer
      title={title}
      open={open}
      onClose={() => {
        if (!loading) {
          onCancel();
        }
      }}
      width={600}
      destroyOnClose
      extra={
        <Space>
          <Button
            onClick={() => {
              void form.validateFields().then(onTest);
            }}
            loading={testing}
            disabled={!canWrite}
          >
            测试接口
          </Button>
          <Button onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button
            type="primary"
            onClick={() => form.submit()}
            loading={loading}
            disabled={!canWrite}
          >
            保存
          </Button>
        </Space>
      }
    >
      <Form<SourceFormValues>
        form={form}
        layout="vertical"
        initialValues={initialValues}
        onFinish={(values) => {
          if (!globalSpiderProxyReady || !values.proxyCollect) {
            values.proxyCollect = false;
          }
          void onSubmit(values);
        }}
        disabled={loading}
      >
        <Form.Item
          label="采集站名称"
          name="name"
          rules={[{ required: true, message: "请输入采集站名称" }]}
        >
          <Input placeholder="请输入采集站名称" />
        </Form.Item>
        <Form.Item
          label="接口地址"
          name="uri"
          rules={[{ required: true, message: "请输入采集站接口地址" }]}
        >
          <Input placeholder="请输入采集站接口地址" />
        </Form.Item>
        {isUriChanged ? (
          <Alert
            showIcon
            type="warning"
            style={{ marginBottom: 16 }}
            title="更换接口地址将重置已采集的线路与分类"
          />
        ) : null}
        <Form.Item label="接口格式" name="format">
          <Radio.Group>
            <Radio value="json">JSON 格式</Radio>
            <Radio value="xml">XML 格式</Radio>
          </Radio.Group>
        </Form.Item>
        <Form.Item
          label="播放链接域名替换规则"
          name="domainReplaceRules"
          tooltip="格式：old.com => new.com"
          rules={[
            {
              validator: async (_, value: string) => {
                if (!value || !value.trim()) {
                  return;
                }
                const bad: string[] = [];
                for (const line of value.split("\n")) {
                  const t = line.trim();
                  if (!t || t.startsWith("#") || t.startsWith("//") || t.startsWith(";")) {
                    continue;
                  }
                  if (!isDomainReplaceRuleLine(t)) {
                    bad.push(t);
                  }
                }
                if (bad.length > 0) {
                  return Promise.reject(
                    new Error(`格式错误: ${bad.join("；")}`),
                  );
                }
              },
            },
          ]}
        >
          <Input.TextArea
            rows={3}
            placeholder={`每行一条，例如：\nhd.domain1.com => hd.domain2.com`}
          />
        </Form.Item>
        <Form.Item
          label="海报图源"
          name="isPosterSource"
          valuePropName="checked"
        >
          <Switch checkedChildren="开启" unCheckedChildren="关闭" />
        </Form.Item>

        <Form.Item
          label={
            <Space size={6}>
              <span>采集接口代理</span>
              {!globalSpiderProxyReady && (
                <span
                  style={{
                    fontSize: 12,
                    color: "var(--ant-color-text-tertiary, #8c8c8c)",
                    fontWeight: "normal",
                  }}
                >
                  （系统网络代理未开启）
                </span>
              )}
            </Space>
          }
          name="proxyCollect"
          valuePropName="checked"
        >
          <Switch
            checkedChildren="开启"
            unCheckedChildren="直连"
            disabled={!globalSpiderProxyReady}
          />
        </Form.Item>

        <Form.Item label="是否启用" name="state" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="禁用" />
        </Form.Item>
      </Form>
    </Drawer>
  );
}
