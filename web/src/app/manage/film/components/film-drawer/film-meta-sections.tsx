"use client";

import React from "react";
import { Tag, Form, Input, InputNumber, Row, Col } from "antd";
import {
  UserOutlined,
  GlobalOutlined,
  DatabaseOutlined,
  ContainerOutlined,
} from "@ant-design/icons";
import styles from "./index.module.less";

const { TextArea } = Input;

export default function FilmMetaSections() {
  return (
    <>
      {/* 1. 演职阵容 */}
      <div className={styles.drawerSectionCard}>
        <div className={styles.drawerSectionTitle}>
          <div className={styles.titleLeft}>
            <UserOutlined className={styles.sectionIcon} />
            <span>演职阵容</span>
          </div>
          <Tag color="purple" style={{ margin: 0, borderRadius: 4 }}>
            主创
          </Tag>
        </div>

        <Row gutter={[16, 12]}>
          <Col xs={24} sm={8}>
            <Form.Item label="导演" name="director" className={styles.formItemReset}>
              <Input placeholder="导演" allowClear />
            </Form.Item>
          </Col>
          <Col xs={24} sm={8}>
            <Form.Item label="主演" name="actor" className={styles.formItemReset}>
              <Input placeholder="主演" allowClear />
            </Form.Item>
          </Col>
          <Col xs={24} sm={8}>
            <Form.Item label="编剧" name="writer" className={styles.formItemReset}>
              <Input placeholder="编剧" allowClear />
            </Form.Item>
          </Col>
        </Row>
      </div>

      {/* 2. 发行档案 */}
      <div className={styles.drawerSectionCard}>
        <div className={styles.drawerSectionTitle}>
          <div className={styles.titleLeft}>
            <GlobalOutlined className={styles.sectionIcon} />
            <span>发行档案</span>
          </div>
          <Tag color="cyan" style={{ margin: 0, borderRadius: 4 }}>
            发行
          </Tag>
        </div>

        <Row gutter={[16, 12]}>
          <Col xs={24} sm={8}>
            <Form.Item label="上映日期" name="releaseDate" className={styles.formItemReset}>
              <Input placeholder="YYYY-MM-DD" allowClear />
            </Form.Item>
          </Col>
          <Col xs={24} sm={8}>
            <Form.Item label="制作地区" name="area" className={styles.formItemReset}>
              <Input placeholder="地区" allowClear />
            </Form.Item>
          </Col>
          <Col xs={24} sm={8}>
            <Form.Item label="语言" name="language" className={styles.formItemReset}>
              <Input placeholder="语言" allowClear />
            </Form.Item>
          </Col>
          <Col xs={24} sm={8}>
            <Form.Item label="上映年份" name="year" className={styles.formItemReset}>
              <Input placeholder="YYYY" allowClear />
            </Form.Item>
          </Col>
          <Col xs={24} sm={8}>
            <Form.Item label="检索首字母" name="initial" className={styles.formItemReset}>
              <Input placeholder="首字母" allowClear />
            </Form.Item>
          </Col>
          <Col xs={24} sm={8}>
            <Form.Item label="剧情标签" name="classTag" className={styles.formItemReset}>
              <Input placeholder="标签" allowClear />
            </Form.Item>
          </Col>
        </Row>
      </div>

      {/* 3. 状态指标 */}
      <div className={styles.drawerSectionCard}>
        <div className={styles.drawerSectionTitle}>
          <div className={styles.titleLeft}>
            <DatabaseOutlined className={styles.sectionIcon} />
            <span>状态指标</span>
          </div>
          <Tag color="orange" style={{ margin: 0, borderRadius: 4 }}>
            指标
          </Tag>
        </div>

        <Row gutter={[16, 12]}>
          <Col xs={24} sm={6}>
            <Form.Item label="影片状态" name="state" className={styles.formItemReset}>
              <Input placeholder="影片状态 (如正片/预告)" allowClear />
            </Form.Item>
          </Col>
          <Col xs={24} sm={6}>
            <Form.Item label="热度" name="hits" className={styles.formItemReset}>
              <InputNumber style={{ width: "100%" }} placeholder="0" />
            </Form.Item>
          </Col>
          <Col xs={24} sm={6}>
            <Form.Item label="豆瓣 ID" name="dbId" className={styles.formItemReset}>
              <InputNumber style={{ width: "100%" }} placeholder="0" />
            </Form.Item>
          </Col>
          <Col xs={24} sm={6}>
            <Form.Item label="豆瓣评分" name="dbScore" className={styles.formItemReset}>
              <Input placeholder="豆瓣评分" allowClear />
            </Form.Item>
          </Col>
        </Row>
      </div>

      {/* 4. 剧情简介 */}
      <div className={styles.drawerSectionCard}>
        <div className={styles.drawerSectionTitle}>
          <div className={styles.titleLeft}>
            <ContainerOutlined className={styles.sectionIcon} />
            <span>剧情简介</span>
          </div>
        </div>

        <Form.Item name="content" noStyle>
          <TextArea rows={4} placeholder="剧情简介" style={{ resize: "vertical" }} />
        </Form.Item>
      </div>
    </>
  );
}
