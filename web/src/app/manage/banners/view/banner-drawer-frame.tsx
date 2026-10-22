"use client";

import React from "react";
import {
  Flex,
  Form,
  Input,
  Image as AntImage,
  Typography,
  Space,
  Button,
  Switch,
  Upload,
} from "antd";
import {
  UploadOutlined,
  PictureOutlined,
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

      <Flex gap={20} justify="center" align="flex-start" style={{ padding: "4px 0 12px" }}>
        <div className={styles.previewCol}>
          <Text type="secondary" style={{ fontSize: 12 }}>
            竖版封面 (2:3)
          </Text>
          <div className={styles.posterFrame}>
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
              <div className={styles.slideEmptyBox}>
                <PictureOutlined style={{ fontSize: 22, color: "var(--ant-color-text-quaternary)" }} />
                <Text type="secondary" style={{ fontSize: 12 }}>
                  无封面
                </Text>
              </div>
            )}
          </div>
        </div>

        <div className={styles.previewCol}>
          <Text type="secondary" style={{ fontSize: 12 }}>
            横屏大图 (16:9)
          </Text>
          <div className={styles.slideFrame}>
            {preview.previewSlide ? (
              <AntImage
                src={preview.previewSlide}
                width="100%"
                height="100%"
                styles={{ root: { width: "100%", height: "100%", display: "block" } }}
                style={{ objectFit: "cover" }}
                fallback={FALLBACK_IMG}
              />
            ) : (
              <div className={styles.slideEmptyBox}>
                <PictureOutlined style={{ fontSize: 22, color: "var(--ant-color-text-quaternary)" }} />
                <Text type="secondary" style={{ fontSize: 12 }}>
                  无横屏大图
                </Text>
              </div>
            )}
          </div>
        </div>
      </Flex>

      {watchedIsCustomPic && (
        <Space direction="vertical" size={12} style={{ width: "100%", marginTop: 8 }}>
          <Form.Item label="竖版封面" required style={{ marginBottom: 0 }}>
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <Form.Item
                name="picture"
                noStyle
                rules={[{ required: true, message: "请输入竖版封面地址" }]}
              >
                <Input placeholder="封面 URL" allowClear style={{ flex: 1 }} />
              </Form.Item>
              <Upload
                showUploadList={false}
                accept={IMAGE_UPLOAD_ACCEPT}
                customRequest={(o) => onCustomUpload(o, "picture")}
              >
                <Button>上传</Button>
              </Upload>
              <Button onClick={() => onOpenPicker("picture")}>
                选图
              </Button>
            </div>
          </Form.Item>

          <Form.Item label="横屏大图" style={{ marginBottom: 0 }}>
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <Form.Item name="pictureSlide" noStyle>
                <Input placeholder="横屏大图 URL" allowClear style={{ flex: 1 }} />
              </Form.Item>
              <Upload
                showUploadList={false}
                accept={IMAGE_UPLOAD_ACCEPT}
                customRequest={(o) => onCustomUpload(o, "pictureSlide")}
              >
                <Button>上传</Button>
              </Upload>
              <Button onClick={() => onOpenPicker("pictureSlide")}>
                选图
              </Button>
              {slideUrl && <Button onClick={onClearSlide}>清空</Button>}
            </div>
          </Form.Item>
        </Space>
      )}
    </div>
  );
}
