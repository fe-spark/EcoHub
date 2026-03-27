/**
 * useAppMessage
 * 在 Ant Design App 组件的 Context 内使用 message，
 * 避免静态 message.xxx() 无法消费主题 Context 的警告。
 *
 * 使用方式：
 *   const { message } = useAppMessage();
 *   message.error("xxx");
 */
import { App } from "antd";

export function useAppMessage() {
  const { message, modal, notification } = App.useApp();
  return { message, modal, notification };
}
