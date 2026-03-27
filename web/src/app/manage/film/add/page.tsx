"use client";

import React, { useState, useEffect, Suspense } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import {
  Form,
  Input,
  Select,
  Button,
  Upload,
  Typography,
  InputNumber,
  Space,
  Spin,
  Card,
  Row,
  Col,
} from "antd";
import {
  UploadOutlined,
  SaveOutlined,
  ClearOutlined,
  ArrowLeftOutlined,
  InfoCircleOutlined,
  UserOutlined,
  GlobalOutlined,
  DatabaseOutlined,
  PlayCircleOutlined,
  ContainerOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/api";
import { useAppMessage } from "@/lib/useAppMessage";
import styles from "./page.module.less";

const { TextArea } = Input;

function FilmAddForm() {
  const [form] = Form.useForm();
  const [categories, setCategories] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [fetching, setFetching] = useState(false);
  const searchParams = useSearchParams();
  const router = useRouter();
  const id = searchParams.get("id");
  const { message } = useAppMessage();

  useEffect(() => {
    // Get category tree for the selector
    ApiGet("/manage/film/class/tree").then((resp: any) => {
      if (resp.code === 0) {
        let list: any[] = [];
        resp.data.children?.forEach((parent: any) => {
          if (parent.children && parent.children.length > 0) {
            list = [...list, ...parent.children];
          }
        });
        setCategories(list);
      }
    });

    // If ID is present, fetch film details
    if (id) {
      setFetching(true);
      ApiGet(`/filmDetail`, { id })
        .then((resp: any) => {
          if (resp.code === 0 && resp.data?.detail) {
            const detail = resp.data.detail;
            const desc = detail.descriptor || {};

            // Reconstruct playLink string from playList
            // playList is [[{episode, link}, ...], ...]
            let playLinkStr = "";
            if (detail.playList && detail.playList.length > 0) {
              const mainList = detail.playList[0];
              playLinkStr = mainList
                .map((item: any) => `${item.episode}$${item.link}`)
                .join("#");
            }

            form.setFieldsValue({
              id: detail.id,
              cid: detail.cid,
              pid: detail.pid,
              name: detail.name,
              picture: detail.picture,
              subTitle: desc.subTitle,
              initial: desc.initial,
              classTag: desc.classTag,
              director: desc.director,
              actor: desc.actor,
              writer: desc.writer,
              remarks: desc.remarks,
              releaseDate: desc.releaseDate,
              area: desc.area,
              lang: desc.language,
              year: desc.year,
              state: desc.state,
              dbId: desc.dbId,
              dbScore: desc.dbScore,
              hits: desc.hits,
              playForm: detail.playFrom?.join(",") || "",
              content: desc.content,
              playLink: playLinkStr,
            });
          } else {
            message.error("获取影片详情失败");
          }
        })
        .finally(() => setFetching(false));
    }
  }, [id, form, message]);

  const handleClassChange = (value: number) => {
    const selected = categories.find((c) => c.id === value);
    if (selected) {
      form.setFieldsValue({
        cid: selected.id,
        pid: selected.pid,
        cName: selected.name,
      });
    }
  };

  const onFinish = async (values: any) => {
    setLoading(true);
    try {
      const payload = {
        ...values,
        id: id ? Number(id) : 0,
        dbId: Number(values.dbId) || 0,
        hits: Number(values.hits) || 0,
      };

      const resp = await ApiPost("/manage/film/add", payload);
      if (resp.code === 0) {
        message.success(id ? "影视更新成功" : "影片添加成功");
        if (!id) {
          form.resetFields();
        }
      } else {
        message.error(resp.msg);
      }
    } finally {
      setLoading(false);
    }
  };

  const customUpload = async (options: any) => {
    const { file, onSuccess, onError } = options;
    const formData = new FormData();
    formData.append("file", file);

    try {
      const resp = await ApiPost("/manage/file/upload", formData);
      if (resp.code === 0) {
        message.success(resp.msg);
        form.setFieldValue("picture", resp.data);
        onSuccess(resp.data);
      } else {
        message.error(resp.msg);
        onError(resp.msg);
      }
    } catch (err: any) {
      message.error("上传失败");
      onError(err);
    }
  };

  if (fetching) {
    return (
      <div className={styles.loadingContainer}>
        <Spin size="large" description="正在加载影片数据..." />
      </div>
    );
  }

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <div className={styles.headerLeft}>
          <Button
            type="text"
            icon={<ArrowLeftOutlined />}
            onClick={() => router.back()}
          >
            返回列表
          </Button>
          <Typography.Title level={4} className={styles.title}>
            {id ? "修改影片详情" : "录入新影片"}
          </Typography.Title>
        </div>
        <Space size="middle">
          {!id && (
            <Button
              icon={<ClearOutlined />}
              onClick={() => form.resetFields()}
              style={{ fontWeight: 500 }}
            >
              清空重填
            </Button>
          )}
          <Button
            type="primary"
            icon={<SaveOutlined />}
            onClick={() => form.submit()}
            loading={loading}
            style={{ paddingLeft: 24, paddingRight: 24, fontWeight: 600 }}
          >
            {id ? "确认保存更新" : "立即提交"}
          </Button>
        </Space>
      </div>

      <Form
        form={form}
        layout="vertical"
        onFinish={onFinish}
        className={`${styles.form} ${styles.formCompact}`}
        initialValues={{
          dbId: 0,
          hits: 0,
        }}
        requiredMark="optional"
      >
        <Space direction="vertical" size={32} style={{ width: "100%" }}>
          <Card
            title={
              <Space>
                <InfoCircleOutlined
                  style={{ color: "var(--ant-color-primary)" }}
                />
                基础信息
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
                <Form.Item
                  label="影片名称"
                  name="name"
                  rules={[{ required: true, message: "请输入名称" }]}
                >
                  <Input placeholder="请输入影片名称" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="影片别名" name="subTitle">
                  <Input placeholder="如: 英文名、又名" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item
                  label="所属分类"
                  name="cid"
                  rules={[{ required: true, message: "请选择分类" }]}
                >
                  <Select
                    placeholder="请选择"
                    onChange={handleClassChange}
                    options={categories.map((c: any) => ({
                      label: c.name,
                      value: c.id,
                    }))}
                  />
                </Form.Item>
              </Col>

              {/* Hidden fields captured by category selection */}
              <Form.Item name="pid" hidden>
                <Input />
              </Form.Item>
              <Form.Item name="cName" hidden>
                <Input />
              </Form.Item>

              <Col xs={24} lg={12} xl={16}>
                <Form.Item
                  label="影片海报"
                  name="picture"
                >
                  <Input
                    placeholder="输入图片URL或上传"
                    addonAfter={
                      <Upload customRequest={customUpload} showUploadList={false}>
                        <Button
                          icon={<UploadOutlined />}
                          type="text"
                          size="small"
                        >
                          上传封面
                        </Button>
                      </Upload>
                    }
                  />
                </Form.Item>
              </Col>
            </Row>
          </Card>

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
                <DatabaseOutlined
                  style={{ color: "var(--ant-color-primary)" }}
                />
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
                <ContainerOutlined
                  style={{ color: "var(--ant-color-primary)" }}
                />
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

          <Card
            title={
              <Space>
                <PlayCircleOutlined
                  style={{ color: "var(--ant-color-primary)" }}
                />
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
              noStyle
              extra="格式: 章节$链接 (多个以 # 分隔)"
            >
              <TextArea
                rows={8}
                placeholder="示例: &#10;第01集$https://url/1.m3u8#第02集$https://url/2.m3u8"
              />
            </Form.Item>
          </Card>
        </Space>
      </Form>
    </div>
  );
}

export default function FilmAddPage() {
  return (
    <Suspense
      fallback={
        <div className={styles.loadingContainer}>
          <Spin size="large" />
        </div>
      }
    >
      <FilmAddForm />
    </Suspense>
  );
}
