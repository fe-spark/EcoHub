"use client";

import React, { useState, useEffect, useRef, Suspense } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import {
  Form,
  Input,
  Select,
  Button,
  Upload,
  InputNumber,
  Space,
  Spin,
  Card,
  Row,
  Col,
  Divider,
  Flex,
  Tag,
  Radio,
  Image as AntImage,
  Typography,
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
  PictureOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import ManagePageHeader from "@/app/manage/components/page-header";
import ImagePicker from "@/app/manage/components/image-picker";
import {
  IMAGE_UPLOAD_ACCEPT,
  isAllowedImageFile,
} from "@/lib/imageUpload";
import styles from "./index.module.less";

const { TextArea } = Input;
const { Text } = Typography;

function FilmAddForm() {
  const [form] = Form.useForm();
  const [categories, setCategories] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const searchParams = useSearchParams();
  const router = useRouter();
  const id = searchParams.get("id");
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const watchedFollowPosterSource = Form.useWatch("followPosterSource", form);
  const watchedPicture = Form.useWatch("picture", form);

  const loadedDetailRef = useRef<any>(null);
  const [loadedDetail, setLoadedDetail] = useState<any>(null);

  useEffect(() => {
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

    if (id) {
      setFetching(true);
      ApiGet(`/filmPlayInfo`, { id })
        .then((resp: any) => {
          if (resp.code === 0 && resp.data?.detail) {
            const filmData = resp.data.detail;
            loadedDetailRef.current = filmData;
            setLoadedDetail(filmData);
            const filmDescriptor = filmData.descriptor || {};

            let playLinkStr = "";
            if (filmData.playList && filmData.playList.length > 0) {
              const mainList = filmData.playList[0];
              playLinkStr = mainList
                .map((item: any) => `${item.episode}$${item.link}`)
                .join("#");
            }

            const isCustom = filmData.isCustomPicture === true;
            const customPic = filmData.customPicture || (isCustom ? filmData.picture : "");
            form.setFieldsValue({
              id: filmData.id,
              cid: filmData.cid,
              pid: filmData.pid,
              name: filmData.name,
              followPosterSource: !isCustom,
              picture: customPic,
              subTitle: filmDescriptor.subTitle,
              initial: filmDescriptor.initial,
              classTag: filmDescriptor.classTag,
              director: filmDescriptor.director,
              actor: filmDescriptor.actor,
              writer: filmDescriptor.writer,
              remarks: filmDescriptor.remarks,
              releaseDate: filmDescriptor.releaseDate,
              area: filmDescriptor.area,
              lang: filmDescriptor.language,
              year: filmDescriptor.year,
              state: filmDescriptor.state,
              dbId: filmDescriptor.dbId,
              dbScore: filmDescriptor.dbScore,
              hits: filmDescriptor.hits,
              playForm: filmData.playFrom?.join(",") || "",
              content: filmDescriptor.content,
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
      const isCustom = !values.followPosterSource;
      const customPic = isCustom ? (values.picture || "").trim() : "";
      const sourcePic = loadedDetailRef.current?.picture || "";
      const payload = {
        ...values,
        id: id ? Number(id) : 0,
        dbId: Number(values.dbId) || 0,
        hits: Number(values.hits) || 0,
        isCustomPicture: isCustom,
        customPicture: customPic,
        picture: isCustom ? (sourcePic || customPic) : "",
      };

      const resp = await ApiPost("/manage/film/add", payload);
      if (resp.code === 0) {
        message.success(id ? "影视更新成功" : "影片添加成功");
        if (!id) {
          form.resetFields();
          form.setFieldsValue({ followPosterSource: false });
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
    if (!isAllowedImageFile(file)) {
      message.error("仅支持上传 JPG/JPEG/PNG/WebP/ICO 格式的图片");
      onError?.(new Error("unsupported image type"));
      return;
    }
    const formData = new FormData();
    formData.append("file", file);

    try {
      const resp = await ApiPost("/manage/file/upload", formData);
      if (resp.code === 0) {
        message.success(resp.msg);
        const fullUrl =
          typeof window !== "undefined" && String(resp.data).startsWith("/")
            ? `${window.location.origin}${resp.data}`
            : resp.data;
        form.setFieldValue("picture", fullUrl);
        onSuccess(fullUrl);
      } else {
        message.error(resp.msg);
        onError(resp.msg);
      }
    } catch (err: any) {
      // 拦截器已统一提示，避免重复弹窗
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
    <div className={styles.pageStack}>
      <ManagePageHeader
        title={id ? "修改影片详情" : "录入新影片"}
        description={
          id
            ? "修改主库存影片详情与播放资源。"
            : "手动录入主库存影片信息、剧情详情与播放资源。"
        }
      />

      <Form
        form={form}
        layout="vertical"
        onFinish={onFinish}
        className={`${styles.form} ${styles.formCompact}`}
        initialValues={{
          dbId: 0,
          hits: 0,
          followPosterSource: Boolean(id),
        }}
        requiredMark="optional"
      >
        <Space direction="vertical" size={16} className={styles.formSections}>
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

                <Form.Item name="pid" hidden>
                  <Input />
                </Form.Item>
                <Form.Item name="cName" hidden>
                  <Input />
                </Form.Item>

                <Col xs={24} lg={10} xl={8}>
                  <Form.Item
                    label="是否跟随海报源"
                    name="followPosterSource"
                    tooltip="开启自动同步海报源；关闭可自定义并锁定封面。"
                    initialValue={Boolean(id)}
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

                {watchedFollowPosterSource ? (
                  <Col xs={24} lg={14} xl={16}>
                    <Form.Item label="影片封面">
                      <div
                        style={{
                          padding: "8px 12px",
                          background: "rgba(255, 255, 255, 0.04)",
                          borderRadius: 6,
                          border: "1px dashed var(--ant-color-border)",
                          display: "flex",
                          alignItems: "center",
                          justifyContent: "space-between",
                          minHeight: 32,
                        }}
                      >
                        <Text style={{ fontSize: 13, color: "var(--ant-color-text-secondary)" }}>
                          已开启跟随海报源，将自动匹配最新封面与幻灯图。
                        </Text>
                        {(watchedPicture || loadedDetail?.picture) && (
                          <AntImage
                            src={watchedPicture || loadedDetail?.picture}
                            height={32}
                            style={{ borderRadius: 4, objectFit: "cover" }}
                            fallback={FALLBACK_IMG}
                          />
                        )}
                      </div>
                    </Form.Item>
                  </Col>
                ) : (
                  <Col xs={24} lg={14} xl={16}>
                    <Form.Item
                      label="封面图片地址"
                      name="picture"
                      rules={[{ required: true, message: "请输入自定义封面图片地址或上传" }]}
                      tooltip="自定义封面，锁定后不被海报源或采集覆盖。"
                    >
                      <Input
                        className={styles.posterInput}
                        placeholder="输入图片URL或上传"
                        addonAfter={
                          <Space size={4}>
                            <Upload
                              customRequest={customUpload}
                              showUploadList={false}
                              accept={IMAGE_UPLOAD_ACCEPT}
                              disabled={!canWrite}
                            >
                              <Button
                                icon={<UploadOutlined />}
                                type="text"
                                size="small"
                              >
                                上传封面
                              </Button>
                            </Upload>
                            <Button
                              icon={<PictureOutlined />}
                              type="text"
                              size="small"
                              disabled={!canWrite}
                              onClick={() => setPickerOpen(true)}
                            >
                              选图
                            </Button>
                          </Space>
                        }
                      />
                    </Form.Item>
                  </Col>
                )}
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

          <Divider className={styles.formDivider} />
          <Flex
            justify="space-between"
            align="center"
            wrap="wrap"
            gap={12}
            className={styles.footerActions}
          >
            <Button
              type="text"
              icon={<ArrowLeftOutlined />}
              onClick={() => router.back()}
              className={styles.backButton}
            >
              返回影片列表
            </Button>
            <Space wrap className={styles.submitActions}>
              {!id ? (
                <Button icon={<ClearOutlined />} onClick={() => form.resetFields()}>
                  清空重填
                </Button>
              ) : null}
              <Button
                type="primary"
                icon={<SaveOutlined />}
                onClick={() => form.submit()}
                loading={loading}
                disabled={!canWrite}
              >
                {id ? "确认保存更新" : "立即提交"}
              </Button>
            </Space>
          </Flex>
        </Space>
      </Form>

      <ImagePicker
        open={pickerOpen}
        onCancel={() => setPickerOpen(false)}
        onSelect={(link) => {
          form.setFieldValue("picture", link);
          setPickerOpen(false);
        }}
      />
    </div>
  );
}

export default function FilmAddPageView() {
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
