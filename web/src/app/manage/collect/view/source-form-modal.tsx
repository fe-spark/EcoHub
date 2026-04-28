import { Button, Form, Input, InputNumber, Modal, Radio, Switch } from "antd";
import { useMemo } from "react";
import type { SourceFormValues } from "./types";

interface SourceFormModalProps {
  open: boolean;
  mode: "add" | "edit";
  loading: boolean;
  form: ReturnType<typeof Form.useForm<SourceFormValues>>[0];
  onCancel: () => void;
  onSubmit: (values: SourceFormValues) => Promise<void> | void;
  onTest: () => void;
}

export default function SourceFormModal(props: SourceFormModalProps) {
  const { open, mode, loading, form, onCancel, onSubmit, onTest } = props;
  const currentGrade = Form.useWatch("grade", form);

  const title = useMemo(
    () => (mode === "add" ? "新增采集站点" : "编辑采集站点"),
    [mode],
  );

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
      maskClosable={!loading}
      destroyOnHidden
      footer={[
        <Button key="test" onClick={onTest}>
          测试接口
        </Button>,
        <Button key="cancel" onClick={onCancel} disabled={loading}>
          取消
        </Button>,
        <Button key="ok" type="primary" onClick={() => form.submit()} loading={loading}>
          {mode === "add" ? "添加站点" : "保存修改"}
        </Button>,
      ]}
    >
      <Form<SourceFormValues>
        form={form}
        layout="vertical"
        disabled={loading}
        onFinish={onSubmit}
      >
        <Form.Item label="资源名称" name="name" rules={[{ required: true, message: "请输入资源名称" }]}>
          <Input placeholder="例如：某资源站" />
        </Form.Item>
        <Form.Item label="接口地址" name="uri" rules={[{ required: true, message: "请输入接口地址" }]}>
          <Input placeholder="请输入资源站采集链接" />
        </Form.Item>
        <Form.Item label="站点类型" name="grade">
          <Radio.Group
            optionType="button"
            buttonStyle="solid"
            onChange={(event) => {
              if (event.target.value === 1) {
                form.setFieldValue("syncPictures", false);
              }
            }}
            options={[
              { label: "主站点", value: 0 },
              { label: "附属站点", value: 1 },
            ]}
          />
        </Form.Item>
        <Form.Item
          label="请求间隔"
          name="interval"
          tooltip="单次请求的额外间隔时间，单位毫秒；0 代表不限制。"
        >
          <InputNumber min={0} step={100} style={{ width: "100%" }} addonAfter="ms" />
        </Form.Item>
        <Form.Item
          label="图片同步"
          name="syncPictures"
          valuePropName="checked"
          extra={currentGrade === 1 ? "附属站默认不允许开启图片同步。" : "仅主站建议开启图片同步。"}
        >
          <Switch checkedChildren="开启" unCheckedChildren="关闭" disabled={currentGrade === 1} />
        </Form.Item>
        <Form.Item label="是否启用" name="state" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="禁用" />
        </Form.Item>
      </Form>
    </Modal>
  );
}
