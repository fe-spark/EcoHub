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
  const { open, mode, loading, testing, initialValues, formNonce, onCancel, onSubmit, onTest } =
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
          {mode === "add" ? "添加采集站" : "保存修改"}
        </Button>,
      ]}
    >
      <Form<SourceFormValues>
        key={formNonce}
        form={form}
        layout="vertical"
        disabled={loading}
        preserve={false}
        initialValues={initialValues}
        onFinish={onSubmit}
      >
        <Form.Item
          label="采集站名称"
          name="name"
          rules={[{ required: true, message: "请输入采集站名称" }]}
        >
          <Input placeholder="例如：某采集站" />
        </Form.Item>
        <Form.Item label="接口地址" name="uri" rules={[{ required: true, message: "请输入接口地址" }]}>
          <Input placeholder="请输入采集站接口地址" />
        </Form.Item>
        <Form.Item label="采集站类型" name="grade">
          <Radio.Group
            optionType="button"
            buttonStyle="solid"
            options={[
              { label: "主采集站", value: 0 },
              { label: "附属采集站", value: 1 },
            ]}
          />
        </Form.Item>
        <Form.Item
          label="请求间隔"
          tooltip="单次请求的额外间隔时间，单位毫秒；0 代表不限制。"
        >
          <Space.Compact block>
            <Form.Item name="interval" noStyle>
              <InputNumber min={0} step={100} style={{ width: "100%" }} />
            </Form.Item>
            <Button disabled tabIndex={-1}>
              ms
            </Button>
          </Space.Compact>
        </Form.Item>
        <Form.Item label="采集时长" name="cd" tooltip="单次采集的时间范围，保存后作为该采集站的默认采集时长。">
          <Select
            options={collectDuration.map((item) => ({ label: item.label, value: item.time }))}
          />
        </Form.Item>
        <Form.Item label="是否启用" name="state" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="禁用" />
        </Form.Item>
      </Form>
    </Modal>
  );
}
