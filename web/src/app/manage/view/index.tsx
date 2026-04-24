"use client";

import React, { useEffect } from "react";
import { Card, Col, Row, Space, Tag, Typography } from "antd";
import {
  AppstoreOutlined,
  DatabaseOutlined,
  PictureOutlined,
  SettingOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import ManagePageShell from "../components/page-shell";
import styles from "./index.module.less";

const { Title, Text } = Typography;

export default function ManagePageView() {
  const { message: _message } = useAppMessage();
  const { config: siteInfo } = useSiteConfig();
  const welcomeText = siteInfo?.siteName
    ? `欢迎使用 ${siteInfo.siteName} 后台管理系统`
    : "欢迎使用后台管理系统";

  useEffect(() => {
    ApiGet("/manage/index").then((_resp) => {
      // 避免首页加载时弹出冗余的"后台管理中心"提示
    });
  }, []);

  return (
    <ManagePageShell
      eyebrow="工作台"
      title="管理后台"
      description="先完成站点与采集配置，再整理分类和影片内容，最后补充图片素材与首页展示资源。"
    >
      <div className={styles.dashboard}>
        <Card className={styles.welcomeCard}>
          <Space direction="vertical" size={8}>
            <Tag color="processing" className={styles.tag}>
              快速开始
            </Tag>
            <Title level={4} className={styles.sectionTitle}>
              {welcomeText}
            </Title>
            <Text className={styles.mutedText}>
              如果你是第一次进入后台，建议按下面的顺序完成初始化，这样后面的分类、采集和影片管理都会更顺。
            </Text>
          </Space>
        </Card>

        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}>
            <Card className={styles.guideCard}>
              <Space direction="vertical" size={16} className={styles.fullWidth}>
                <div className={styles.cardHeader}>
                  <SettingOutlined className={styles.cardIcon} />
                  <div>
                    <Title level={5} className={styles.cardTitle}>
                      推荐使用顺序
                    </Title>
                    <Text className={styles.mutedText}>按这个顺序操作，最不容易出现数据链路断开。</Text>
                  </div>
                </div>

                <div className={styles.stepList}>
                  <div className={styles.stepItem}>
                    <span className={styles.stepIndex}>1</span>
                    <div>
                      <div className={styles.stepTitle}>网站配置</div>
                      <div className={styles.stepDesc}>先到“系统设置 / 网站配置”完成站点名称、Logo、基础开关等配置。</div>
                    </div>
                  </div>
                  <div className={styles.stepItem}>
                    <span className={styles.stepIndex}>2</span>
                    <div>
                      <div className={styles.stepTitle}>采集站点</div>
                      <div className={styles.stepDesc}>在“采集中心 / 采集站点”配置主站和附属站。切换主站会重建当前主站分类与主库存。</div>
                    </div>
                  </div>
                  <div className={styles.stepItem}>
                    <span className={styles.stepIndex}>3</span>
                    <div>
                      <div className={styles.stepTitle}>分类管理</div>
                      <div className={styles.stepDesc}>进入“内容管理 / 分类管理”检查当前主站分类树，调整显示状态和排序；需要回到原始树时使用“重置分类”。</div>
                    </div>
                  </div>
                  <div className={styles.stepItem}>
                    <span className={styles.stepIndex}>4</span>
                    <div>
                      <div className={styles.stepTitle}>影片列表与手动录入</div>
                      <div className={styles.stepDesc}>通过“影片列表”查看主库存，通过“手动录入”补充或修正影片详情。</div>
                    </div>
                  </div>
                  <div className={styles.stepItem}>
                    <span className={styles.stepIndex}>5</span>
                    <div>
                      <div className={styles.stepTitle}>图片素材与首页封面</div>
                      <div className={styles.stepDesc}>最后整理站内图片素材，并在“系统设置 / 首页封面”配置首页展示内容。</div>
                    </div>
                  </div>
                </div>
              </Space>
            </Card>
          </Col>

          <Col xs={24} lg={12}>
            <Card className={styles.guideCard}>
              <Space direction="vertical" size={16} className={styles.fullWidth}>
                <div className={styles.cardHeader}>
                  <ThunderboltOutlined className={styles.cardIcon} />
                  <div>
                    <Title level={5} className={styles.cardTitle}>
                      常用入口说明
                    </Title>
                    <Text className={styles.mutedText}>下面是后台最常用的几个模块，以及它们分别负责什么。</Text>
                  </div>
                </div>

                <div className={styles.entryList}>
                  <div className={styles.entryItem}>
                    <AppstoreOutlined className={styles.entryIcon} />
                    <div>
                      <div className={styles.entryTitle}>内容管理</div>
                      <div className={styles.stepDesc}>维护影片列表、手动录入、分类排序和显示状态。</div>
                    </div>
                  </div>
                  <div className={styles.entryItem}>
                    <DatabaseOutlined className={styles.entryIcon} />
                    <div>
                      <div className={styles.entryTitle}>采集中心</div>
                      <div className={styles.stepDesc}>配置采集站、查看失败记录、管理计划任务。</div>
                    </div>
                  </div>
                  <div className={styles.entryItem}>
                    <PictureOutlined className={styles.entryIcon} />
                    <div>
                      <div className={styles.entryTitle}>图片素材</div>
                      <div className={styles.stepDesc}>上传、预览和整理站内会用到的封面图与素材图。</div>
                    </div>
                  </div>
                  <div className={styles.entryItem}>
                    <SettingOutlined className={styles.entryIcon} />
                    <div>
                      <div className={styles.entryTitle}>系统设置</div>
                      <div className={styles.stepDesc}>管理网站配置、首页封面和后台账号。</div>
                    </div>
                  </div>
                </div>
              </Space>
            </Card>
          </Col>
        </Row>
      </div>
    </ManagePageShell>
  );
}
