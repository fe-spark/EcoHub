"use client";

import React from "react";
import {
  Tag,
  Form,
  Input,
  Select,
  TreeSelect,
  Switch,
  Space,
  Button,
  Upload,
  Image as AntImage,
  Row,
  Col,
  Typography,
  type FormInstance,
} from "antd";
import {
  InfoCircleOutlined,
  CompassOutlined,
  UndoOutlined,
  UploadOutlined,
  PictureOutlined,
} from "@ant-design/icons";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { IMAGE_UPLOAD_ACCEPT } from "@/lib/imageUpload";
import styles from "./index.module.less";

interface FilmBasicSectionProps {
  form: FormInstance;
  filmId?: number | null;
  sources: { id: string; name: string; state?: boolean }[];
  categoryTreeData: any[];
  categoryLoading: boolean;
  tmdbEnabled: boolean;
  tmdbSnapshot: Record<string, any> | null;
  canWrite: boolean;
  watchedPicture?: string;
  watchedFollowPosterSource?: boolean;
  loadedDetail: any;
  onOpenTmdb: () => void;
  onUndoTmdb: () => void;
  onOpenPicker: () => void;
  onCustomUpload: (options: any) => Promise<void>;
  onSourceChange: (sourceId: string) => void;
  onClassChange: (cid: any) => void;
}

const { Text } = Typography;

export default function FilmBasicSection({
  form,
  filmId,
  sources,
  categoryTreeData,
  categoryLoading,
  tmdbEnabled,
  tmdbSnapshot,
  canWrite,
  watchedPicture,
  watchedFollowPosterSource,
  loadedDetail,
  onOpenTmdb,
  onUndoTmdb,
  onOpenPicker,
  onCustomUpload,
  onSourceChange,
  onClassChange,
}: FilmBasicSectionProps) {
  const isCustomMode = watchedFollowPosterSource === false;
  const posterSource = String(loadedDetail?.picture || (!isCustomMode ? watchedPicture : "") || "").trim();
  const currentPreview = isCustomMode
    ? watchedPicture || loadedDetail?.customPicture || loadedDetail?.picture
    : loadedDetail?.picture || watchedPicture;

  const handleCustomSwitch = (checked: boolean) => {
    form.setFieldValue("followPosterSource", !checked);
    if (checked) {
      const currentPic = form.getFieldValue("picture");
      if (!currentPic && loadedDetail?.picture) {
        form.setFieldValue("picture", loadedDetail.picture);
      }
    }
  };

  return (
    <div className={styles.drawerSectionCard}>
      <div className={styles.drawerSectionTitle}>
        <div className={styles.titleLeft}>
          <InfoCircleOutlined className={styles.sectionIcon} />
          <span>基础档案</span>
        </div>
        <Space size={8} align="center">
          <span className={styles.switchLabel}>自定义封面</span>
          <Form.Item name="followPosterSource" noStyle>
            <Switch
              size="small"
              checked={isCustomMode}
              onChange={handleCustomSwitch}
            />
          </Form.Item>
        </Space>
      </div>

      {/* 顶部影视档案横幅卡 */}
      <div className={styles.heroProfileCard}>
        <div className={styles.posterColumn}>
          <div className={styles.posterContainer}>
            <AntImage
              width={110}
              height={154}
              style={{ objectFit: "cover", display: "block" }}
              wrapperStyle={{ width: 110, height: 154, display: "block" }}
              src={currentPreview}
              alt="封面"
              fallback={FALLBACK_IMG}
              preview={{ mask: "预览" }}
            />
            <Tag
              color={!isCustomMode ? "blue" : "purple"}
              className={styles.posterTag}
            >
              {!isCustomMode ? "跟随源" : "自定义"}
            </Tag>
          </div>
        </div>

        <div className={styles.heroProfileInfo}>
          {/* 影片名称与 TMDB 刮削 */}
          <Form.Item label="影片名称" className={styles.formItemReset} required>
            <Space.Compact style={{ width: "100%" }}>
              <Form.Item
                name="name"
                noStyle
                rules={[{ required: true, message: "请输入影片名称" }]}
              >
                <Input placeholder="影片名称" allowClear />
              </Form.Item>
              {tmdbEnabled && (
                <Button
                  icon={<CompassOutlined />}
                  style={{ color: "#722ed1", borderColor: "#722ed1", fontWeight: 500 }}
                  onClick={onOpenTmdb}
                  disabled={!canWrite}
                >
                  TMDB 刮削
                </Button>
              )}
              {tmdbSnapshot && (
                <Button icon={<UndoOutlined />} onClick={onUndoTmdb}>
                  撤销
                </Button>
              )}
            </Space.Compact>
          </Form.Item>

          {/* 影片别名 */}
          <Form.Item label="影片别名" name="subTitle" className={styles.formItemReset}>
            <Input placeholder="别名 / 英文名" allowClear />
          </Form.Item>
        </div>
      </div>

      {/* 下方属性网格：归属采集站与所属分类并排联动 */}
      <div className={styles.metaAttributeGrid}>
        <Row gutter={[16, 12]}>
          {/* 1. 归属采集站 & 所属分类（联动框排在一起） */}
          <Col xs={24} sm={12}>
            <Form.Item
              label="归属采集站"
              name="sourceId"
              className={styles.formItemReset}
              rules={[{ required: true, message: "请选择采集站" }]}
            >
              <Select
                style={{ width: "100%" }}
                placeholder="选择采集站"
                onChange={onSourceChange}
                disabled={Boolean(filmId)}
                options={sources.map((s) => ({
                  label: s.state === false ? `${s.name} (停用)` : s.name,
                  value: s.id,
                }))}
              />
            </Form.Item>
          </Col>

          <Col xs={24} sm={12}>
            <Form.Item
              label="所属分类"
              name="cid"
              className={styles.formItemReset}
              rules={[{ required: true, message: "请选择分类" }]}
            >
              <TreeSelect
                style={{ width: "100%" }}
                placeholder={categoryLoading ? "加载中..." : "选择分类"}
                loading={categoryLoading}
                disabled={categoryLoading}
                onChange={onClassChange}
                treeData={categoryTreeData}
                treeDefaultExpandAll
                treeLine={{ showLeafIcon: false }}
                popupMatchSelectWidth={false}
                allowClear
                showSearch
                treeNodeFilterProp="title"
                styles={{ popup: { root: { minWidth: 240, maxHeight: 380, overflow: "auto" } } }}
              />
            </Form.Item>
          </Col>

          {/* 2. 更新备注 */}
          <Col xs={24} sm={12}>
            <Form.Item label="更新备注" name="remarks" className={styles.formItemReset}>
              <Input placeholder="如: 完结、更新至第10集" allowClear />
            </Form.Item>
          </Col>

          {/* 3. 封面图片地址与上传/选图（宽裕整洁排布，避免狭小按钮挤爆） */}
          <Col xs={24} sm={12}>
            {isCustomMode ? (
              <Form.Item label="封面图片地址" className={styles.formItemReset} required>
                <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                  <Form.Item
                    name="picture"
                    noStyle
                    rules={[{ required: true, message: "请输入封面地址" }]}
                  >
                    <Input
                      placeholder="自定义封面图片 URL"
                      allowClear
                      style={{ flex: 1 }}
                    />
                  </Form.Item>
                  <Upload
                    customRequest={onCustomUpload}
                    showUploadList={false}
                    accept={IMAGE_UPLOAD_ACCEPT}
                    disabled={!canWrite}
                  >
                    <Button icon={<UploadOutlined />}>上传</Button>
                  </Upload>
                  <Button
                    icon={<PictureOutlined />}
                    disabled={!canWrite}
                    onClick={onOpenPicker}
                  >
                    选图
                  </Button>
                </div>
              </Form.Item>
            ) : (
              <Form.Item
                label={<span data-no-optional>海报源地址</span>}
                className={styles.formItemReset}
              >
                <div className={styles.posterSourceBox}>
                  {posterSource ? (
                    <Text
                      copyable={{ text: posterSource }}
                      ellipsis={{ tooltip: posterSource }}
                      className={styles.posterSourceText}
                    >
                      {posterSource}
                    </Text>
                  ) : (
                    <Text type="secondary">暂无</Text>
                  )}
                </div>
              </Form.Item>
            )}
          </Col>
        </Row>
      </div>
    </div>
  );
}
