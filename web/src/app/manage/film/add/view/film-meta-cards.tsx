"use client";

import React from "react";
import { Card, Row, Col, Form, Input, InputNumber, Space } from "antd";
import {
  UserOutlined,
  GlobalOutlined,
  DatabaseOutlined,
  ContainerOutlined,
  PlayCircleOutlined,
} from "@ant-design/icons";

const { TextArea } = Input;

interface FilmMetaCardsProps {
  isNew: boolean;
  styles: Record<string, string>;
}

export default function FilmMetaCards({ isNew, styles }: FilmMetaCardsProps) {
  return (
    <>
      <Card
        title={
          <Space>
            <UserOutlined style={{ color: "var(--ant-color-primary)" }} />
            演职人员
          </Space>
        }
        className={styles.sectionCard}
        styles={{
          header: {
            background: "rgba(255, 255, 255, 0.02)",
            borderBottom: "1px solid var(--ant-color-border-secondary)",
          },
        }}
      >
        <Row gutter={[40, 0]} className={styles.formRow}>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="导演" name="director">
              <Input placeholder="多个以逗号分隔" />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="主演" name="actor">
              <Input placeholder="多个以逗号分隔" />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="作者/编剧" name="writer">
              <Input placeholder="多个以逗号分隔" />
            </Form.Item>
          </Col>
        </Row>
      </Card>

      <Card
        title={
          <Space>
            <GlobalOutlined style={{ color: "var(--ant-color-primary)" }} />
            发行与元数据
          </Space>
        }
        className={styles.sectionCard}
        styles={{
          header: {
            background: "rgba(255, 255, 255, 0.02)",
            borderBottom: "1px solid var(--ant-color-border-secondary)",
          },
        }}
      >
        <Row gutter={[40, 0]} className={styles.formRow}>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="上映日期" name="releaseDate">
              <Input placeholder="YYYY-MM-DD" />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="制作地区" name="area">
              <Input placeholder="如: 中国大陆, 美国" />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="语言" name="lang">
              <Input placeholder="如: 国语, 英语" />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="上映年份" name="year">
              <Input placeholder="YYYY" />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="检索首字母" name="initial">
              <Input placeholder="大写字母" />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="剧情标签" name="classTag">
              <Input placeholder="如: 动作, 冒险" />
            </Form.Item>
          </Col>
        </Row>
      </Card>

      <Card
        title={
          <Space>
            <DatabaseOutlined style={{ color: "var(--ant-color-primary)" }} />
            状态与外部数据
          </Space>
        }
        className={styles.sectionCard}
        styles={{
          header: {
            background: "rgba(255, 255, 255, 0.02)",
            borderBottom: "1px solid var(--ant-color-border-secondary)",
          },
        }}
      >
        <Row gutter={[40, 0]} className={styles.formRow}>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="更新备注" name="remarks">
              <Input placeholder="如: 完结, 第10集" />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="影片状态" name="state">
              <Input placeholder="如: 正片, 预告" />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="影片热度" name="hits">
              <InputNumber style={{ width: "100%" }} />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="播放来源标识" name="playForm">
              <Input placeholder="如: m3u8_list" />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="豆瓣 ID" name="dbId">
              <InputNumber style={{ width: "100%" }} />
            </Form.Item>
          </Col>
          <Col xs={24} lg={12} xl={8}>
            <Form.Item label="豆瓣评分" name="dbScore">
              <Input />
            </Form.Item>
          </Col>
        </Row>
      </Card>

      <Card
        title={
          <Space>
            <ContainerOutlined style={{ color: "var(--ant-color-primary)" }} />
            剧情详情
          </Space>
        }
        className={styles.sectionCard}
        styles={{
          header: {
            background: "rgba(255, 255, 255, 0.02)",
            borderBottom: "1px solid var(--ant-color-border-secondary)",
          },
        }}
      >
        <Form.Item name="content" noStyle>
          <TextArea rows={6} placeholder="输入剧情详细描述..." />
        </Form.Item>
      </Card>

      {isNew && (
        <Card
          title={
            <Space>
              <PlayCircleOutlined style={{ color: "var(--ant-color-primary)" }} />
              播放资源
            </Space>
          }
          className={styles.sectionCard}
          styles={{
            header: {
              background: "rgba(255, 255, 255, 0.02)",
              borderBottom: "1px solid var(--ant-color-border-secondary)",
            },
          }}
        >
          <Form.Item
            name="playLink"
            rules={[{ required: true, message: "新增影片必须录入至少一条有效播放地址" }]}
            extra="格式: 章节$链接 (多个以 # 分隔)，如: 正片$https://example.com/play.m3u8"
          >
            <TextArea
              rows={4}
              placeholder="示例: &#10;第01集$https://url/1.m3u8#第02集$https://url/2.m3u8"
            />
          </Form.Item>
        </Card>
      )}
    </>
  );
}
