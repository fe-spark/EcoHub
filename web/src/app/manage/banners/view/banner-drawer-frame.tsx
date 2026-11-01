"use client";

import React from "react";
import {
  Form,
  Input,
  Image as AntImage,
  Typography,
  Space,
  Button,
  Switch,
  Upload,
  Tag,
  Flex,
} from "antd";
import {
  UploadOutlined,
  PictureOutlined,
  DeleteOutlined,
} from "@ant-design/icons";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { IMAGE_UPLOAD_ACCEPT } from "@/lib/imageUpload";
import { UploadFieldName } from "./types";
import { BannerPreviewState } from "./banner-modal-utils";
import styles from "./banner-drawer.module.less";

const { Text } = Typography;

interface BannerDrawerFrameProps {
  preview: BannerPreviewState;
  watchedIsCustomPic?: boolean;
  slideUrl?: string;
  onCustomSwitchChange: (checked: boolean) => void;
  onCustomUpload: (options: any, fieldName: UploadFieldName) => Promise<void>;
  onOpenPicker: (target: UploadFieldName) => void;
  onClearSlide: () => void;
}

export default function BannerDrawerFrame({
  preview,
  watchedIsCustomPic,
  slideUrl,
  onCustomSwitchChange,
  onCustomUpload,
  onOpenPicker,
  onClearSlide,
}: BannerDrawerFrameProps) {
  const hasSlide = Boolean(preview.previewSlide?.trim());

  return (
    <div className={styles.drawerSectionCard}>
      <div className={styles.drawerSectionTitle}>
        <span>轮播画幅配置</span>
        <Space size={8} align="center">
          <span style={{ fontSize: 13, color: "var(--ant-color-text-secondary)" }}>
            自定义封面与大图
          </span>
          <Form.Item name="isCustomPic" valuePropName="checked" noStyle>
            <Switch size="small" onChange={onCustomSwitchChange} />
          </Form.Item>
        </Space>
      </div>

      <div className={styles.frameStack}>
        {/* 1. 核心焦点：16:9 横屏轮播大图 */}
        <div className={styles.heroBlock}>
          <div className={styles.heroHeader}>
            <div className={styles.heroTitle}>
              <PictureOutlined style={{ color: "var(--ant-color-primary)" }} />
              <span>横屏大图</span>
              <Tag color="blue" style={{ margin: 0, borderRadius: 4 }}>
                16:9 首页轮播主图
              </Tag>
            </div>
            <Tag
              color={hasSlide ? "success" : "warning"}
              style={{ margin: 0, borderRadius: 4 }}
            >
              {hasSlide ? "已配置" : "未配置"}
            </Tag>
          </div>

          <div className={styles.heroCanvas}>
            {hasSlide ? (
              <AntImage
                src={preview.previewSlide}
                width="100%"
                height="100%"
                styles={{ root: { width: "100%", height: "100%", display: "block" } }}
                style={{ objectFit: "cover" }}
                fallback={FALLBACK_IMG}
              />
            ) : (
              <div className={styles.heroEmpty}>
                <PictureOutlined style={{ fontSize: 36, opacity: 0.4 }} />
                <Text type="secondary" style={{ fontSize: 13 }}>
                  未配置 16:9 横屏轮播大图
                </Text>
              </div>
            )}
          </div>

          {watchedIsCustomPic ? (
            <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 4 }}>
              <div className={styles.heroActionRow}>
                <Upload
                  showUploadList={false}
                  accept={IMAGE_UPLOAD_ACCEPT}
                  customRequest={(o) => onCustomUpload(o, "pictureSlide")}
                >
                  <Button size="small" icon={<UploadOutlined />}>
                    上传大图
                  </Button>
                </Upload>
                <Button
                  size="small"
                  icon={<PictureOutlined />}
                  onClick={() => onOpenPicker("pictureSlide")}
                >
                  从图库选取
                </Button>
                {slideUrl && (
                  <Button
                    size="small"
                    danger
                    icon={<DeleteOutlined />}
                    onClick={onClearSlide}
                  >
                    清空大图
                  </Button>
                )}
              </div>
              <Form.Item name="pictureSlide" noStyle>
                <Input placeholder="输入或粘贴横屏大图 URL" allowClear />
              </Form.Item>
            </div>
          ) : (
            <div className={styles.urlReadonlyHint} title={preview.previewSlide}>
              {preview.previewSlide
                ? `当前生效: ${preview.previewSlide}`
                : "当前跟随片库原图，开启右上角自定义可单独上传或设置"}
            </div>
          )}
        </div>

        {/* 2. 次要画幅：2:3 列表竖版封面 */}
        <div className={styles.posterCompactBlock}>
          <div className={styles.posterMiniFrame}>
            {preview.previewPicture ? (
              <AntImage
                src={preview.previewPicture}
                width="100%"
                height="100%"
                styles={{ root: { width: "100%", height: "100%", display: "block" } }}
                style={{ objectFit: "cover" }}
                fallback={FALLBACK_IMG}
              />
            ) : (
              <PictureOutlined style={{ fontSize: 18, color: "var(--ant-color-text-quaternary)" }} />
            )}
          </div>

          <div className={styles.posterMiniInfo}>
            <div className={styles.posterMiniHead}>
              <div className={styles.posterMiniTitle}>
                <span>竖版封面</span>
                <Tag style={{ margin: 0, borderRadius: 4 }}>2:3 列表海报</Tag>
              </div>
              <Tag
                color={watchedIsCustomPic ? "purple" : "default"}
                style={{ margin: 0, borderRadius: 4 }}
              >
                {watchedIsCustomPic ? "自定义" : "片库源"}
              </Tag>
            </div>

            {watchedIsCustomPic ? (
              <Flex gap={8} align="center">
                <Form.Item
                  name="picture"
                  noStyle
                  rules={[{ required: true, message: "请输入竖版封面地址" }]}
                >
                  <Input placeholder="输入或粘贴竖版封面 URL" allowClear style={{ flex: 1 }} />
                </Form.Item>
                <Upload
                  showUploadList={false}
                  accept={IMAGE_UPLOAD_ACCEPT}
                  customRequest={(o) => onCustomUpload(o, "picture")}
                >
                  <Button size="small" icon={<UploadOutlined />}>
                    上传
                  </Button>
                </Upload>
                <Button
                  size="small"
                  icon={<PictureOutlined />}
                  onClick={() => onOpenPicker("picture")}
                >
                  选图
                </Button>
              </Flex>
            ) : (
              <div className={styles.urlReadonlyHint} title={preview.previewPicture}>
                {preview.previewPicture ? `当前生效: ${preview.previewPicture}` : "跟随片库源默认"}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
