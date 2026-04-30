import { Form, Input, InputNumber, Select, Space, Tag } from "antd";

import {
  cronFieldHelp,
  dayOptions,
  getTaskTypeText,
  hourOptions,
  minuteOptions,
  type ScheduleMode,
  weekOptions,
} from "../utils/schedule";

interface EditScheduleFormProps {
  editModel: number | undefined;
}

export default function EditScheduleForm({ editModel }: EditScheduleFormProps) {
  return (
    <>
      <Form.Item name="id" hidden>
        <Input />
      </Form.Item>
      <Form.Item name="model" hidden>
        <Input />
      </Form.Item>
      <Form.Item name="time" hidden>
        <Input />
      </Form.Item>
      <Form.Item label="任务标识" name="id">
        <Input disabled />
      </Form.Item>
      <Form.Item label="任务类型">
        <Tag color="cyan">{getTaskTypeText(Number(editModel))}</Tag>
      </Form.Item>
      <Form.Item
        label="运行方式"
        name="scheduleMode"
        initialValue="interval"
        rules={[{ required: true, message: "请选择运行方式" }]}
      >
        <Select
          options={[
            { value: "interval", label: "按分钟循环" },
            { value: "daily", label: "每天固定时间" },
            { value: "weekly", label: "每周固定时间" },
            { value: "monthly", label: "每月固定时间" },
            { value: "advanced", label: "高级 Cron" },
          ]}
        />
      </Form.Item>
      <Form.Item shouldUpdate noStyle>
        {({ getFieldValue }) => {
          const scheduleMode = getFieldValue("scheduleMode") as ScheduleMode;

          if (scheduleMode === "interval") {
            return (
              <Form.Item
                label="循环间隔"
                name="intervalMinutes"
                rules={[{ required: true, message: "请输入循环分钟数" }]}
                extra="适合自动更新这类持续轮询任务。"
              >
                <InputNumber min={1} max={59} addonAfter="分钟" style={{ width: 220 }} />
              </Form.Item>
            );
          }

          if (scheduleMode === "daily") {
            return (
              <Space size={12} wrap style={{ width: "100%" }}>
                <Form.Item label="小时" name="scheduleHour" rules={[{ required: true, message: "请选择小时" }]}>
                  <Select options={hourOptions} style={{ width: 180 }} />
                </Form.Item>
                <Form.Item label="分钟" name="scheduleMinute" rules={[{ required: true, message: "请选择分钟" }]}>
                  <Select options={minuteOptions} style={{ width: 180 }} />
                </Form.Item>
              </Space>
            );
          }

          if (scheduleMode === "weekly") {
            return (
              <Space size={12} wrap style={{ width: "100%" }}>
                <Form.Item label="星期" name="scheduleWeek" rules={[{ required: true, message: "请选择星期" }]}>
                  <Select options={weekOptions} style={{ width: 180 }} />
                </Form.Item>
                <Form.Item label="小时" name="scheduleHour" rules={[{ required: true, message: "请选择小时" }]}>
                  <Select options={hourOptions} style={{ width: 180 }} />
                </Form.Item>
                <Form.Item label="分钟" name="scheduleMinute" rules={[{ required: true, message: "请选择分钟" }]}>
                  <Select options={minuteOptions} style={{ width: 180 }} />
                </Form.Item>
              </Space>
            );
          }

          if (scheduleMode === "monthly") {
            return (
              <Space size={12} wrap style={{ width: "100%" }}>
                <Form.Item label="日期" name="scheduleDay" rules={[{ required: true, message: "请选择日期" }]}>
                  <Select options={dayOptions} style={{ width: 180 }} />
                </Form.Item>
                <Form.Item label="小时" name="scheduleHour" rules={[{ required: true, message: "请选择小时" }]}>
                  <Select options={hourOptions} style={{ width: 180 }} />
                </Form.Item>
                <Form.Item label="分钟" name="scheduleMinute" rules={[{ required: true, message: "请选择分钟" }]}>
                  <Select options={minuteOptions} style={{ width: 180 }} />
                </Form.Item>
              </Space>
            );
          }

          return (
            <Form.Item
              label="Cron"
              name="cronSpec"
              rules={[{ required: true, message: "请输入 Cron" }]}
              extra={`${cronFieldHelp}；完整格式为“秒 分 时 日 月 周”，例如 0 */20 * * * ?`}
            >
              <Input placeholder="例如 0 */20 * * * ?" />
            </Form.Item>
          );
        }}
      </Form.Item>
    </>
  );
}
