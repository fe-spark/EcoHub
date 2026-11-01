import type { ReactNode } from "react";
import type { ArgsProps, JointContent, MessageInstance, MessageType, NoticeType } from "antd/es/message/interface";

let current: MessageInstance | null = null;

function isArgs(content: JointContent): content is ArgsProps {
  return Boolean(content) && typeof content === "object" && "content" in (content as ArgsProps);
}

function open(type: NoticeType, content: JointContent, duration?: number | VoidFunction, onClose?: VoidFunction): MessageType {
  const node: ReactNode = isArgs(content) ? content.content : content;
  const config: ArgsProps = isArgs(content)
    ? { ...content, type }
    : {
        type,
        content: node,
        ...(typeof duration === "function" ? { onClose: duration } : { duration, onClose }),
      };
  if (current) return current.open(config);
  let closeFn: (() => void) | undefined;
  const closePromise = import("antd").then(({ message }) => {
    const closer = message.open(config);
    closeFn = closer;
    return closer;
  });
  const result = (() => {
    closeFn?.();
  }) as MessageType;
  result.then = (onFulfilled, onRejected) => closePromise.then(onFulfilled, onRejected);
  return result;
}

export const message: MessageInstance = {
  success: (content, duration, onClose) => open("success", content, duration, onClose),
  error: (content, duration, onClose) => open("error", content, duration, onClose),
  info: (content, duration, onClose) => open("info", content, duration, onClose),
  warning: (content, duration, onClose) => open("warning", content, duration, onClose),
  loading: (content, duration, onClose) => open("loading", content, duration, onClose),
  open: (args) => (current ? current.open(args) : open(args.type || "info", args)),
  destroy: (key) => current?.destroy(key),
};

export function setAppMessage(instance: MessageInstance) {
  current = instance;
}
