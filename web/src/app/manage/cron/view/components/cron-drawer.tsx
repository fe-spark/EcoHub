import { Button, Drawer, Flex, Form, Space } from "antd";
import type { FormInstance } from "antd/es/form";
import EditScheduleForm from "./edit-schedule-form";

interface CronDrawerProps {
  open: boolean;
  canWrite: boolean;
  form: FormInstance;
  editModel: number | undefined;
  onClose: () => void;
  onSubmit: () => void;
}

export default function CronDrawer({
  open,
  canWrite,
  form,
  editModel,
  onClose,
  onSubmit,
}: CronDrawerProps) {
  return (
    <Drawer
      title="修改运行时间"
      open={open}
      onClose={onClose}
      width={520}
      destroyOnClose
      extra={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button
            type="primary"
            disabled={!canWrite}
            onClick={onSubmit}
          >
            保存
          </Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical">
        <EditScheduleForm editModel={editModel} />
      </Form>
    </Drawer>
  );
}
