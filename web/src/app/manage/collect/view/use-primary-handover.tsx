"use client";

import { useCallback, useRef, useState } from "react";
import { Modal, Radio, Space, Typography } from "antd";
import type { FilmSource } from "./types";

export interface HandoverChoice {
  newPrimaryId: string;
  confirmClear: boolean;
}

interface HandoverRequest {
  title: string;
  leavingName: string;
  otherEnabled: FilmSource[];
}

const CLEAR_CHOICE = "__clear_primary__";

/** 停用/删除当前主站前的交接选择（D6）：选新主站，或在无其他启用站时确认置空。 */
export function usePrimaryHandover() {
  const [request, setRequest] = useState<HandoverRequest | null>(null);
  const [choice, setChoice] = useState<string>("");
  const resolveRef = useRef<((c: HandoverChoice | null) => void) | null>(null);

  const ask = useCallback((req: HandoverRequest) => {
    return new Promise<HandoverChoice | null>((resolve) => {
      resolveRef.current = resolve;
      setChoice(req.otherEnabled[0]?.id ?? CLEAR_CHOICE);
      setRequest(req);
    });
  }, []);

  const settle = useCallback((value: HandoverChoice | null) => {
    const resolve = resolveRef.current;
    resolveRef.current = null;
    setRequest(null);
    resolve?.(value);
  }, []);

  const modalNode = request ? (
    <Modal
      title={request.title}
      open
      okText="确定"
      cancelText="取消"
      okButtonProps={{ disabled: !choice }}
      onOk={() =>
        settle(
          choice === CLEAR_CHOICE
            ? { newPrimaryId: "", confirmClear: true }
            : { newPrimaryId: choice, confirmClear: false },
        )
      }
      onCancel={() => settle(null)}
    >
      <Space direction="vertical" size={12}>
        {request.otherEnabled.length > 0 ? (
          <Radio.Group value={choice} onChange={(e) => setChoice(String(e.target.value))}>
            <Space direction="vertical">
              {request.otherEnabled.map((s) => (
                <Radio key={s.id} value={s.id}>
                  新主站：{s.name}
                </Radio>
              ))}
            </Space>
          </Radio.Group>
        ) : (
          <Typography.Text type="warning">确认置空主站？</Typography.Text>
        )}
      </Space>
    </Modal>
  ) : null;

  return { ask, modalNode };
}
