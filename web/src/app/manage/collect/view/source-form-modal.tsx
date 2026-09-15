import {
  Button,
  Col,
  Form,
  Input,
  InputNumber,
  Modal,
  Radio,
  Row,
  Select,
  Space,
  Switch,
} from "antd";
import { useEffect, useMemo } from "react";
import { useManagePermission } from "@/lib/manage-permission";
import { collectDuration, SOURCE_FORM_DEFAULTS, type SourceFormValues } from "./types";

interface SourceFormModalProps {
  open: boolean;
  mode: "add" | "edit";
  loading: boolean;
  testing?: boolean;
  initialValues: SourceFormValues;
  formNonce: number;
  onCancel: () => void;
  onSubmit: (values: SourceFormValues) => Promise<void> | void;
  onTest: (values: SourceFormValues) => void;
}

export default function SourceFormModal(props: SourceFormModalProps) {
  const { open, mode, loading, testing, initialValues, formNonce, onCancel, onSubmit, onTest } =
    props;
  const [form] = Form.useForm<SourceFormValues>();
  const { canWrite } = useManagePermission();

  const formSourceType = Form.useWatch("sourceType", form);
  const sourceType = formSourceType ?? initialValues.sourceType ?? "maccms";
  const isWebdav = sourceType === "webdav";

  const title = useMemo(() => {
    if (mode === "add") {
      return isWebdav ? "新增 WebDAV 媒体库" : "新增采集站";
    }
    return isWebdav ? "编辑 WebDAV 媒体库" : "编辑采集站";
  }, [mode, isWebdav]);

  const testBtnText = useMemo(() => (isWebdav ? "测试连通" : "测试接口"), [isWebdav]);

  const submitBtnText = useMemo(() => {
    if (mode === "add") {
      return isWebdav ? "添加媒体库" : "添加采集站";
    }
    return "保存修改";
  }, [mode, isWebdav]);

  useEffect(() => {
    if (!open) {
      return;
    }
    form.resetFields();
    form.setFieldsValue(initialValues);
  }, [open, form, initialValues]);

  const handleTypeChange = (nextType: "maccms" | "webdav") => {
    const curName = form.getFieldValue("name");
    const curState = form.getFieldValue("state");
    if (nextType === "webdav") {
      form.setFieldsValue({
        ...SOURCE_FORM_DEFAULTS,
        sourceType: "webdav",
        name: curName ?? "",
        state: curState ?? false,
        grade: 1,
        webdav: {
          ...SOURCE_FORM_DEFAULTS.webdav,
          serverUrl: "",
          rootPath: "/",
          minFileBytes: 52428800,
        },
      });
    } else {
      form.setFieldsValue({
        ...SOURCE_FORM_DEFAULTS,
        sourceType: "maccms",
        name: curName ?? "",
        state: curState ?? false,
      });
    }
  };

  return (
    <Modal
      title={title}
      open={open}
      width={isWebdav ? 720 : 560}
      onCancel={() => {
        if (!loading) {
          onCancel();
        }
      }}
      onOk={() => form.submit()}
      confirmLoading={loading}
      closable={!loading}
      mask={{ closable: !loading }}
      destroyOnHidden
      footer={[
        <Button
          key="test"
          onClick={() => {
            void form.validateFields().then(onTest);
          }}
          loading={testing}
          disabled={!canWrite}
        >
          {testBtnText}
        </Button>,
        <Button key="cancel" onClick={onCancel} disabled={loading}>
          取消
        </Button>,
        <Button
          key="ok"
          type="primary"
          onClick={() => form.submit()}
          loading={loading}
          disabled={!canWrite}
        >
          {submitBtnText}
        </Button>,
      ]}
    >
      <Form<SourceFormValues>
        key={formNonce}
        form={form}
        layout="vertical"
        disabled={loading}
        preserve={false}
        initialValues={initialValues}
        onFinish={onSubmit}
      >
        <Form.Item
          label="站点来源类型"
          name="sourceType"
          tooltip={mode === "edit" ? "编辑状态下不可更改类型" : undefined}
        >
          <Radio.Group
            optionType="button"
            buttonStyle="solid"
            disabled={mode === "edit"}
            onChange={(e) => handleTypeChange(e.target.value)}
            options={[
              { label: "MacCMS 采集站", value: "maccms" },
              { label: "WebDAV 媒体库", value: "webdav" },
            ]}
          />
        </Form.Item>

        {isWebdav ? (
          <>
            <Row gutter={16}>
              <Col span={12}>
                <Form.Item
                  label="媒体库名称"
                  name="name"
                  rules={[
                    { required: true, message: "请输入媒体库名称" },
                    { max: 20, message: "名称不能超过 20 个字符" },
                  ]}
                  tooltip="计入 12 个采集源上限"
                >
                  <Input placeholder="例如：我的影视 NAS" maxLength={20} />
                </Form.Item>
              </Col>
              <Col span={12}>
                <Form.Item
                  label="线路显示名"
                  name={["webdav", "playFromName"]}
                  tooltip="播放器中展示的线路名称，留空则使用媒体库名称"
                >
                  <Input placeholder="私有高清（留空则使用媒体库名）" maxLength={30} />
                </Form.Item>
              </Col>
            </Row>

            <Row gutter={16}>
              <Col span={14}>
                <Form.Item
                  label="WebDAV 服务地址"
                  name={["webdav", "serverUrl"]}
                  rules={[{ required: true, message: "请输入 WebDAV 服务地址" }]}
                  tooltip="包含协议与端口。宿主机 go run 访问 Docker WebDAV 时填 127.0.0.1:宿主机映射端口；EcoHub 也在 Docker 时改用 host.docker.internal 或局域网 IP"
                >
                  <Input placeholder="http://127.0.0.1:5678/dav" />
                </Form.Item>
              </Col>
              <Col span={10}>
                <Form.Item
                  label="根路径"
                  name={["webdav", "rootPath"]}
                  rules={[{ required: true, message: "请输入根路径" }]}
                  tooltip="媒体文件所在的基础目录，如 / 或 /media"
                >
                  <Input placeholder="/" />
                </Form.Item>
              </Col>
            </Row>

            <Row gutter={16}>
              <Col span={12}>
                <Form.Item
                  label="访问用户名"
                  name={["webdav", "username"]}
                  tooltip="无账号认证则留空"
                >
                  <Input placeholder="留空表示无密码认证" />
                </Form.Item>
              </Col>
              <Col span={12}>
                <Form.Item
                  label="访问密码"
                  name={["webdav", "password"]}
                  tooltip="仅编辑修改时填写；留空保持现有密码不变"
                >
                  <Input.Password
                    placeholder={
                      initialValues.webdav?.passwordSet
                        ? "已配置密码，留空保持不变"
                        : "访问密码（留空表示无密码）"
                    }
                  />
                </Form.Item>
              </Col>
            </Row>

            <Row gutter={16}>
              <Col span={12}>
                <Form.Item
                  label="媒体类型"
                  name={["webdav", "mediaType"]}
                  rules={[{ required: true, message: "请选择媒体类型（电影或剧集）" }]}
                  tooltip="一季多文件必须选剧集；电影和剧集请拆分为两个源分别管理"
                >
                  <Radio.Group
                    optionType="button"
                    buttonStyle="solid"
                    options={[
                      { label: "电影 (Movie)", value: "movie" },
                      { label: "剧集 (TV)", value: "tv" },
                    ]}
                  />
                </Form.Item>
              </Col>
              <Col span={12}>
                <Form.Item
                  label="最小正片大小"
                  name={["webdav", "minFileBytes"]}
                  tooltip="小于该大小的文件将被跳过，不参与刮削"
                >
                  <Select
                    options={[
                      { label: "50 MB（过滤片头片尾花絮）", value: 52428800 },
                      { label: "20 MB", value: 20971520 },
                      { label: "不过滤大小", value: 0 },
                    ]}
                  />
                </Form.Item>
              </Col>
            </Row>

            <Row gutter={16}>
              <Col span={12}>
                <Form.Item
                  label="TMDB API Key"
                  name={["webdav", "tmdbApiKey"]}
                  tooltip="留空则使用系统环境变量 TMDB_API_KEY 配置"
                >
                  <Input.Password
                    placeholder={
                      initialValues.webdav?.tmdbApiKeySet
                        ? "已配置 Key，留空保持不变"
                        : "留空使用系统全局配置"
                    }
                  />
                </Form.Item>
              </Col>
              <Col span={12}>
                <Form.Item
                  label="TMDB API 代理/BaseURL"
                  name={["webdav", "tmdbBaseUrl"]}
                  tooltip="需包含 /3，留空则使用默认官方地址或系统环境变量"
                >
                  <Input placeholder="https://api.themoviedb.org/3" />
                </Form.Item>
              </Col>
            </Row>

            <Row gutter={16}>
              <Col span={12}>
                <Form.Item
                  label="站点角色"
                  name="grade"
                  tooltip="暂不支持将 WebDAV 设为主站，仅支持附属高清线，须配合已有 MacCMS 主站"
                >
                  <Radio.Group
                    optionType="button"
                    buttonStyle="solid"
                    options={[
                      { label: "主媒体库", value: 0, disabled: true },
                      { label: "附属媒体库", value: 1 },
                    ]}
                  />
                </Form.Item>
              </Col>
              <Col span={6}>
                <Form.Item
                  label="海报图源"
                  name="isPosterSource"
                  valuePropName="checked"
                  tooltip="采集时用其高清海报填充主站对应影片（全局唯一，关闭自动回退主站）。"
                >
                  <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                </Form.Item>
              </Col>
              <Col span={6}>
                <Form.Item label="是否启用" name="state" valuePropName="checked">
                  <Switch checkedChildren="启用" unCheckedChildren="禁用" />
                </Form.Item>
              </Col>
            </Row>
          </>
        ) : (
          <>
            <Form.Item
              label="采集站名称"
              name="name"
              rules={[{ required: true, message: "请输入采集站名称" }]}
            >
              <Input placeholder="例如：某采集站" />
            </Form.Item>
            <Form.Item
              label="接口地址"
              name="uri"
              rules={[{ required: true, message: "请输入接口地址" }]}
            >
              <Input placeholder="请输入采集站接口地址" />
            </Form.Item>
            <Form.Item label="采集站类型" name="grade">
              <Radio.Group
                optionType="button"
                buttonStyle="solid"
                options={[
                  { label: "主采集站", value: 0 },
                  { label: "附属采集站", value: 1 },
                ]}
                onChange={(e) => {
                  if (e.target.value === 0) {
                    form.setFieldValue("isPosterSource", true);
                  }
                }}
              />
            </Form.Item>
            <Form.Item
              label="请求间隔"
              tooltip="单次请求的额外间隔时间，单位毫秒；0 代表不限制。"
            >
              <Space.Compact block>
                <Form.Item name="interval" noStyle>
                  <InputNumber min={0} step={100} style={{ width: "100%" }} />
                </Form.Item>
                <Button disabled tabIndex={-1}>
                  ms
                </Button>
              </Space.Compact>
            </Form.Item>
            <Form.Item
              label="采集时长"
              name="cd"
              tooltip="单次采集的时间范围，保存后作为该采集站的默认采集时长。"
            >
              <Select
                options={collectDuration.map((item) => ({ label: item.label, value: item.time }))}
              />
            </Form.Item>
            <Form.Item
              label="视频域名替换"
              name="domainReplaceRules"
              tooltip="播放和下载链接的域名替换。每行一条，格式：旧域名 => 新域名"
              rules={[
                {
                  validator: async (_, value: string) => {
                    if (!value || !String(value).trim()) {
                      return;
                    }
                    const bad: string[] = [];
                    for (const line of String(value).split("\n")) {
                      const t = line.trim();
                      if (!t || t.startsWith("#") || t.startsWith("//") || t.startsWith(";")) {
                        continue;
                      }
                      const ok =
                        t.includes("=>") ||
                        t.includes("->") ||
                        t.includes(",") ||
                        t.split(/\s+/).length === 2;
                      if (!ok) {
                        bad.push(t);
                      }
                    }
                    if (bad.length > 0) {
                      return Promise.reject(
                        new Error(`无法解析: ${bad.join("；")}。请使用 旧域名 => 新域名`),
                      );
                    }
                  },
                },
              ]}
            >
              <Input.TextArea
                rows={3}
                placeholder={`每行一条，例如：\nhd.ijycnd.com => hd.kuktxu.com`}
              />
            </Form.Item>
            <Form.Item
              label="海报图源"
              name="isPosterSource"
              valuePropName="checked"
              tooltip="采集时用其高清海报填充主站对应影片（全局唯一，关闭自动回退主站）。"
            >
              <Switch checkedChildren="开启" unCheckedChildren="关闭" />
            </Form.Item>
            <Form.Item label="是否启用" name="state" valuePropName="checked">
              <Switch checkedChildren="启用" unCheckedChildren="禁用" />
            </Form.Item>
          </>
        )}
      </Form>
    </Modal>
  );
}
