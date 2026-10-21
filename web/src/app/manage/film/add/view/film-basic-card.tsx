"use client";

import React from "react";
import {
  Card,
  Row,
  Col,
  Form,
  Input,
  Select,
  TreeSelect,
  Radio,
  Space,
  Button,
  Upload,
  Tag,
  Image as AntImage,
  Typography,
} from "antd";
import {
  InfoCircleOutlined,
  UndoOutlined,
  CompassOutlined,
  UploadOutlined,
  PictureOutlined,
} from "@ant-design/icons";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { IMAGE_UPLOAD_ACCEPT } from "@/lib/imageUpload";

const { Text } = Typography;

interface FilmBasicCardProps {
  id: string | null;
  readOnly: boolean;
  sources: { id: string; name: string; state?: boolean }[];
  categoryTreeData: any[];
  categoryLoading: boolean;
  watchedFollowPosterSource: boolean;
  watchedPicture: string;
  loadedDetail: any;
  canWrite: boolean;
  tmdbEnabled: boolean;
  tmdbSnapshot: Record<string, any> | null;
  onUndoTmdb: () => void;
  onOpenTmdb: () => void;
  onSourceChange: (sourceId: string) => void;
  onClassChange: (cid?: number) => void;
  onUpload: (options: any) => void;
  onOpenPicker: () => void;
  styles: Record<string, string>;
}

export default function FilmBasicCard({
  id,
  readOnly,
  sources,
  categoryTreeData,
  categoryLoading,
  watchedFollowPosterSource,
  watchedPicture,
  loadedDetail,
  canWrite,
  tmdbEnabled,
  tmdbSnapshot,
  onUndoTmdb,
  onOpenTmdb,
  onSourceChange,
  onClassChange,
  onUpload,
  onOpenPicker,
  styles,
}: FilmBasicCardProps) {
  return (
    <Card
      title={
        <Space>
          <InfoCircleOutlined style={{ color: "var(--ant-color-primary)" }} />
          基础信息
        </Space>
      }
      extra={
        readOnly ? null : (
          <Space>
            {tmdbSnapshot && (
              <Button icon={<UndoOutlined />} onClick={onUndoTmdb}>
                撤销填入
              </Button>
            )}
            {tmdbEnabled ? (
              <Button
                icon={<CompassOutlined />}
                style={{ color: "#722ed1", borderColor: "#722ed1" }}
                onClick={onOpenTmdb}
                disabled={!canWrite}
              >
                刮削
              </Button>
            ) : null}
          </Space>
        )
      }
      className={styles.sectionCard}
      styles={{
        header: {
          background: "rgba(255, 255, 255, 0.02)",
          borderBottom: "1px solid var(--ant-color-border-secondary)",
        },
      }}
    >
      <Form.Item name="pid" hidden>
        <Input />
      </Form.Item>
      <Form.Item name="cName" hidden>
        <Input />
      </Form.Item>

      <div className={styles.basicInfoLayout}>
        <div className={styles.posterColumn}>
          <div className={styles.posterWrapper}>
            <AntImage
              width="100%"
              height="100%"
              style={{
                width: "100%",
                height: "100%",
                objectFit: "cover",
                display: "block",
              }}
              wrapperStyle={{
                width: "100%",
                height: "100%",
                display: "block",
              }}
              src={watchedPicture || loadedDetail?.picture}
              alt="海报封面"
              fallback={FALLBACK_IMG}
              preview={{ mask: "点击放大" }}
            />
            {watchedFollowPosterSource && (
              <Tag color="blue" className={styles.posterTagBadge}>
                已跟随海报源
              </Tag>
            )}
          </div>
        </div>

        <div className={styles.formColumn}>
          <Row gutter={[24, 0]}>
            <Col xs={24} sm={12}>
              <Form.Item
                label="影片名称"
                name="name"
                rules={[{ required: true, message: "请输入名称" }]}
              >
                <Input placeholder="请输入影片名称" />
              </Form.Item>
            </Col>
            <Col xs={24} sm={12}>
              <Form.Item label="影片别名" name="subTitle">
                <Input placeholder="如: 英文名、又名" />
              </Form.Item>
            </Col>
            <Col xs={24} sm={12}>
              <Form.Item
                label="归属采集站"
                name="sourceId"
                rules={[{ required: true, message: "请选择归属采集站" }]}
                tooltip={id ? "已建档影片归属站已锁定" : "决定影片收录的采集源与分类体系"}
              >
                <Select
                  style={{ width: "100%" }}
                  placeholder="选择采集站"
                  onChange={onSourceChange}
                  disabled={Boolean(id) || readOnly}
                  options={sources.map((s) => ({
                    label: s.state === false ? `${s.name} (已停用)` : s.name,
                    value: s.id,
                  }))}
                />
              </Form.Item>
            </Col>
            <Col xs={24} sm={12}>
              <Form.Item
                label="所属分类"
                name="cid"
                rules={[{ required: true, message: "请选择分类" }]}
              >
                <TreeSelect
                  style={{ width: "100%" }}
                  placeholder={categoryLoading ? "分类加载中..." : "请选择分类"}
                  loading={categoryLoading}
                  disabled={categoryLoading || readOnly}
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

            <Col xs={24}>
              <Form.Item
                label="是否跟随海报源"
                name="followPosterSource"
                tooltip="关闭后保存自定义封面，采集不覆盖。"
              >
                <Radio.Group
                  buttonStyle="solid"
                  options={[
                    { label: "是（跟随海报源）", value: true },
                    { label: "否（自定义封面）", value: false },
                  ]}
                />
              </Form.Item>
            </Col>

            {!watchedFollowPosterSource ? (
              <Col xs={24}>
                <Form.Item
                  label="封面图片地址"
                  tooltip="采集不覆盖。对得上海报源时，前台用那边的最终图。"
                >
                  <Space.Compact style={{ width: "100%" }}>
                    <Form.Item
                      name="picture"
                      noStyle
                      rules={[{ required: true, message: "请输入自定义封面图片地址或上传" }]}
                    >
                      <Input
                        className={styles.posterInput}
                        placeholder="输入图片URL或点击右侧上传/选图"
                      />
                    </Form.Item>
                    {!readOnly && (
                      <Upload
                        customRequest={onUpload}
                        showUploadList={false}
                        accept={IMAGE_UPLOAD_ACCEPT}
                        disabled={!canWrite}
                      >
                        <Button icon={<UploadOutlined />}>上传封面</Button>
                      </Upload>
                    )}
                    {!readOnly && (
                      <Button
                        icon={<PictureOutlined />}
                        disabled={!canWrite}
                        onClick={onOpenPicker}
                      >
                        选图
                      </Button>
                    )}
                  </Space.Compact>
                </Form.Item>
              </Col>
            ) : (
              <Col xs={24}>
                <div
                  style={{
                    padding: "8px 12px",
                    background: "rgba(255, 255, 255, 0.03)",
                    borderRadius: 6,
                    border: "1px dashed var(--ant-color-border)",
                    marginBottom: 20,
                  }}
                >
                  <Text style={{ fontSize: 13, color: "var(--ant-color-text-secondary)", wordBreak: "break-all" }}>
                    {String(loadedDetail?.picture || "").trim() || "暂无封面"}
                  </Text>
                </div>
              </Col>
            )}
          </Row>
        </div>
      </div>
    </Card>
  );
}
