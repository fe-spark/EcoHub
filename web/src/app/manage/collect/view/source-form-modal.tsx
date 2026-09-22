import { Button, Form, Input, InputNumber, Modal, Radio, Select, Space, Switch } from "antd";
import { useEffect, useMemo } from "react";
import { useManagePermission } from "@/lib/manage-permission";
import { collectDuration, type SourceFormValues } from "./types";

interface SourceFormModalProps {
  open: boolean;
  mode: "add" | "edit";
  loading: boolean;
  testing?: boolean;
  initialValues: SourceFormValues;
  formNonce: number;
  onCancel: () => void;
  onSubmit: (values: SourceFormValues) => Promise<void> | void;
  onTest: (values: SourceFormValues) => void;
}

export default function SourceFormModal(props: SourceFormModalProps) {
  const { open, mode, loading, testing, initialValues, onCancel, onSubmit, onTest } =
    props;
  const [form] = Form.useForm<SourceFormValues>();
  const { canWrite } = useManagePermission();
  const title = useMemo(
    () => (mode === "add" ? "新增采集站" : "编辑采集站"),
    [mode],
  );

  useEffect(() => {
    if (!open) {
      return;
    }
    form.resetFields();
    form.setFieldsValue(initialValues);
  }, [open, form, initialValues]);

  return (
    <Modal
      title={title}
      open={open}
      onCancel={() => {
        if (!loading) {
          onCancel();
        }
      }}
      onOk={() => form.submit()}
      confirmLoading={loading}
      closable={!loading}
      mask={{ closable: !loading }}
      destroyOnHidden
      footer={[
        <Button
          key="test"
          onClick={() => {
            void form.validateFields().then(onTest);
          }}
          loading={testing}
          disabled={!canWrite}
        >
          测试接口
        </Button>,
        <Button key="cancel" onClick={onCancel} disabled={loading}>
          取消
        </Button>,
        <Button
          key="ok"
          type="primary"
          onClick={() => form.submit()}
          loading={loading}
          disabled={!canWrite}
        >
          确定
        </Button>,
      ]}
    >
      <Form<SourceFormValues>
        form={form}
        layout="vertical"
        initialValues={initialValues}
        onFinish={onSubmit}
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
        <Form.Item
          label="采集站类型"
          name="grade"
          tooltip="系统只能有一个主采集站。若将当前站点设为主站，原主站会自动降级为附属采集站，并会清空主站数据重新初始化。"
        >
          <Radio.Group>
            <Radio value={0}>主采集站</Radio>
            <Radio value={1}>附属采集站</Radio>
          </Radio.Group>
        </Form.Item>
        <Space style={{ display: "flex" }} align="start">
          <Form.Item
            label="采集时间间隔 (毫秒)"
            name="interval"
            tooltip="每次分页抓取之间的等待时间，单位为毫秒。默认为 0，表示不等待立即抓取下一页；若采集站有防爬频控或返回限流错误，建议设置为 500 ~ 2000 毫秒。"
          >
            <InputNumber min={0} step={100} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item
            label="采集时长"
            name="cd"
            tooltip="定时自动采集与单站采集时，默认抓取多长时间内更新的数据。单位为小时。"
          >
            <Select style={{ width: 140 }}>
              {collectDuration.map((item) => (
                <Select.Option key={item.time} value={item.time}>
                  {item.label}
                </Select.Option>
              ))}
            </Select>
          </Form.Item>
        </Space>
        <Form.Item
          label="播放链接域名替换规则"
          name="domainReplaceRules"
          tooltip={
            "将采集到的播放链接中匹配的域名替换为新域名。例如源站提供防盗链失效链接时，可替换为镜像或有效访问地址。每行一条，格式：old.com => new.com"
          }
          rules={[
            {
              validator: async (_, value: string) => {
                if (!value || !value.trim()) {
                  return;
                }
                const lines = value
                  .split("\n")
                  .map((l) => l.trim())
                  .filter(Boolean);
                const bad: string[] = [];
                for (const line of lines) {
                  const t = line.trim();
                  if (!t) {
                    continue;
                  }
                  const parts = t.split("=>");
                  if (parts.length !== 2 || !parts[0].trim() || !parts[1].trim()) {
                    bad.push(t);
                  }
                }
                if (bad.length > 0) {
                  return Promise.reject(
                    new Error(`无法解析: ${bad.join("；")}。请使用 旧域名 => 新域名`),
                  );
                }
              },
            },
          ]}
        >
          <Input.TextArea
            rows={3}
            placeholder={`每行一条，例如：\nhd.ijycnd.com => hd.kuktxu.com`}
          />
        </Form.Item>
        <Form.Item
          label="海报图源"
          name="isPosterSource"
          valuePropName="checked"
          tooltip="采集时用其高清海报填充主站对应影片（全局唯一，关闭自动回退主站）。"
        >
          <Switch checkedChildren="开启" unCheckedChildren="关闭" />
        </Form.Item>
        <Form.Item label="是否启用" name="state" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="禁用" />
        </Form.Item>
      </Form>
    </Modal>
  );
}
