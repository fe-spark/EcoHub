import { Button, Form, Input, InputNumber, Modal, Radio, Select, Switch, Typography } from "antd";
import { useMemo } from "react";
import { useManagePermission } from "@/lib/manage-permission";
import { collectDuration, type SourceFormValues } from "./types";

const { Text } = Typography;

interface SourceFormModalProps {
  open: boolean;
  mode: "add" | "edit";
  loading: boolean;
  testing?: boolean;
  form: ReturnType<typeof Form.useForm<SourceFormValues>>[0];
  onCancel: () => void;
  onSubmit: (values: SourceFormValues) => Promise<void> | void;
  onTest: () => void;
}

export default function SourceFormModal(props: SourceFormModalProps) {
  const { open, mode, loading, testing, form, onCancel, onSubmit, onTest } =
    props;
  const { canWrite } = useManagePermission();
  const currentGrade = Form.useWatch("grade", form);

  const title = useMemo(
    () => (mode === "add" ? "新增采集站" : "编辑采集站"),
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
      mask={{ closable: !loading }}
      destroyOnHidden
      footer={[
        <Button key="test" onClick={onTest} loading={testing} disabled={!canWrite}>
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
        form={form}
        layout="vertical"
        disabled={loading}
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
            onChange={(event) => {
              if (event.target.value === 1) {
                form.setFieldValue("syncPictures", false);
              }
            }}
            options={[
              { label: "主采集站", value: 0 },
              { label: "附属采集站", value: 1 },
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
        <Form.Item label="采集时长" name="cd" tooltip="单次采集的时间范围，保存后作为该采集站的默认采集时长。">
          <Select
            options={collectDuration.map((item) => ({ label: item.label, value: item.time }))}
          />
        </Form.Item>
        {currentGrade === 0 ? (
          <Form.Item
            label="图片同步"
            name="syncPictures"
            valuePropName="checked"
            extra="采集时把影片封面下载到服务器素材中心，可统一搜索、改名与管理。"
          >
            <Switch checkedChildren="开启" unCheckedChildren="关闭" />
          </Form.Item>
        ) : (
          <Form.Item label="图片同步">
            <Text type="secondary">
              附属采集站仅扩展主站影片的播放源，不产生独立影片与封面，无需图片同步。
            </Text>
          </Form.Item>
        )}
        <Form.Item label="是否启用" name="state" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="禁用" />
        </Form.Item>
      </Form>
    </Modal>
  );
}
