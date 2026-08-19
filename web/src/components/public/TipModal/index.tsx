"use client";

import React from "react";
import { Button, Modal } from "antd";
import { HeartOutlined } from "@ant-design/icons";
import {
  DEFAULT_TIP_MESSAGE,
  DEFAULT_TIP_TITLE,
  visibleTipChannels,
  type TipConfig,
} from "@/lib/tip";
import styles from "./index.module.less";

interface TipModalProps {
  open: boolean;
  tip?: TipConfig | null;
  onClose: () => void;
}

export default function TipModal({ open, tip, onClose }: TipModalProps) {
  const channels = visibleTipChannels(tip);
  const title = tip?.title?.trim() || DEFAULT_TIP_TITLE;
  const message = tip?.message?.trim() || DEFAULT_TIP_MESSAGE;

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      centered
      width={channels.length > 1 ? 520 : 360}
      className={styles.modal}
      destroyOnHidden
    >
      <div className={styles.head}>
        <HeartOutlined className={styles.heart} />
        <h3 className={styles.title}>{title}</h3>
        <p className={styles.message}>{message}</p>
      </div>
      {channels.length === 0 ? (
        <p className={styles.empty}>尚未配置收款码或外链，前台不会显示入口。</p>
      ) : (
        <div className={styles.grid}>
          {channels.map((channel, index) => (
            <div key={`${channel.key}-${index}`} className={styles.channel}>
              {channel.qrImage ? (
                <>
                  <div className={styles.qrWrap}>
                    {/* eslint-disable-next-line @next/next/no-img-element */}
                    <img className={styles.qr} src={channel.qrImage} alt={`${channel.label}收款码`} />
                  </div>
                  <span className={styles.label}>{channel.label}</span>
                  <p className={styles.hint}>长按或截图保存后扫码</p>
                </>
              ) : (
                <span className={styles.label}>{channel.label}</span>
              )}
              {channel.link ? (
                <Button
                  className={styles.linkBtn}
                  type="primary"
                  href={channel.link}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  前往赞赏
                </Button>
              ) : null}
            </div>
          ))}
        </div>
      )}
    </Modal>
  );
}
