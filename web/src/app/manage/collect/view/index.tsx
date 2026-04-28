"use client";

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Flex,
  Form,
  Input,
  Modal,
  Popconfirm,
  Row,
  Select,
  Statistic,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  DeleteOutlined,
  EditOutlined,
  LoadingOutlined,
  PauseOutlined,
  PlusOutlined,
  PoweroffOutlined,
  SendOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import BatchCollectModal from "./batch-collect-modal";
import SourceFormModal from "./source-form-modal";
import {
  collectDuration,
  type BatchOption,
  type FilmSource,
  type SourceFormValues,
} from "./types";
import styles from "./index.module.less";

interface CollectListItemResponse extends Partial<FilmSource> {
  id: string;
  name: string;
  uri: string;
}

function normalizeSource(item: CollectListItemResponse): FilmSource {
  return {
    id: item.id,
    name: item.name,
    uri: item.uri,
    syncPictures: Boolean(item.syncPictures),
    state: Boolean(item.state),
    grade: Number(item.grade ?? 1),
    interval: Number(item.interval ?? 0),
    cd: Number(item.cd ?? 24),
  };
}

export default function CollectManagePageView() {
  const { message } = useAppMessage();
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const [activeCollectIds, setActiveCollectIds] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const timerRef = useRef<NodeJS.Timeout | null>(null);

  const [sourceForm] = Form.useForm<SourceFormValues>();
  const [sourceModalMode, setSourceModalMode] = useState<"add" | "edit">("add");
  const [sourceModalOpen, setSourceModalOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const [batchOpen, setBatchOpen] = useState(false);
  const [batchIds, setBatchIds] = useState<string[]>([]);
  const [batchTime, setBatchTime] = useState(24);
  const [batchOptions, setBatchOptions] = useState<BatchOption[]>([]);

  const [clearOpen, setClearOpen] = useState(false);
  const [password, setPassword] = useState("");

  const stats = useMemo(
    () => ({
      total: siteList.length,
      enabled: siteList.filter((item) => item.state).length,
      running: activeCollectIds.length,
      masters: siteList.filter((item) => item.grade === 0).length,
    }),
    [activeCollectIds.length, siteList],
  );

  const masterSite = useMemo(
    () => siteList.find((item) => item.grade === 0) ?? null,
    [siteList],
  );

  const masterStatus = useMemo(() => {
    if (stats.masters === 1) {
      return { text: "正常", color: "success" as const };
    }
    if (stats.masters === 0) {
      return { text: "缺少主站", color: "warning" as const };
    }
    return { text: `${stats.masters} 个主站`, color: "error" as const };
  }, [stats.masters]);

  const getCollectList = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/collect/list");
      if (resp.code === 0) {
        const list = Array.isArray(resp.data)
          ? resp.data.map((item: CollectListItemResponse) =>
              normalizeSource(item),
            )
          : [];
        setSiteList(list);
      } else {
        message.error(resp.msg || "采集站列表加载失败");
      }
    } finally {
      setLoading(false);
    }
  }, [message]);

  const getCollectingState = useCallback(async () => {
    const resp = await ApiGet("/manage/collect/collecting/state", undefined);
    if (resp.code === 0 && Array.isArray(resp.data)) {
      setActiveCollectIds(resp.data as string[]);
    }
  }, []);

  useEffect(() => {
    void getCollectList();
    void getCollectingState();
    timerRef.current = setInterval(() => {
      void getCollectingState();
    }, 4000);
    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current);
      }
    };
  }, [getCollectList, getCollectingState]);

  const updateSiteListItem = useCallback(
    (id: string, updater: (record: FilmSource) => FilmSource) => {
      setSiteList((current) =>
        current.map((item) => (item.id === id ? updater(item) : item)),
      );
    },
    [],
  );

  const changeSourceState = async (record: FilmSource) => {
    const resp = await ApiPost("/manage/collect/change", {
      id: record.id,
      state: record.state,
      syncPictures: record.syncPictures,
    });
    if (resp.code !== 0) {
      message.error(resp.msg || "状态更新失败");
      await getCollectList();
    }
  };

  const startTask = async (record: FilmSource) => {
    const resp = await ApiPost("/manage/spider/start", {
      id: record.id,
      time: record.cd || 24,
      batch: false,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      await getCollectingState();
      return;
    }
    message.error(resp.msg || "启动采集失败");
  };

  const stopTask = async (id: string) => {
    const resp = await ApiPost("/manage/collect/stop", { id });
    if (resp.code === 0) {
      message.success("已请求停止任务");
      await getCollectingState();
      return;
    }
    message.error(resp.msg || "停止任务失败");
  };

  const delSource = async (id: string) => {
    const resp = await ApiPost("/manage/collect/del", { id });
    if (resp.code === 0) {
      message.success(resp.msg);
      await getCollectList();
      return;
    }
    message.error(resp.msg || "删除采集站失败");
  };

  const openAddDialog = () => {
    setSourceModalMode("add");
    setEditingId(null);
    sourceForm.resetFields();
    sourceForm.setFieldsValue({
      grade: 1,
      syncPictures: false,
      state: false,
      interval: 0,
      name: "",
      uri: "",
    });
    setSourceModalOpen(true);
  };

  const openEditDialog = async (id: string) => {
    setSourceModalMode("edit");
    setEditingId(id);
    const resp = await ApiGet("/manage/collect/find", { id });
    if (resp.code === 0 && resp.data) {
      sourceForm.setFieldsValue({
        name: String(resp.data.name ?? ""),
        uri: String(resp.data.uri ?? ""),
        syncPictures: Boolean(resp.data.syncPictures),
        state: Boolean(resp.data.state),
        grade: Number(resp.data.grade ?? 1),
        interval: Number(resp.data.interval ?? 0),
      });
      setSourceModalOpen(true);
      return;
    }
    message.error(resp.msg || "获取站点信息失败");
  };

  const handleSubmitSource = async (values: SourceFormValues) => {
    setSubmitting(true);
    try {
      const resp = await ApiPost(
        sourceModalMode === "add"
          ? "/manage/collect/add"
          : "/manage/collect/update",
        sourceModalMode === "add" ? values : { ...values, id: editingId },
      );
      if (resp.code === 0) {
        message.success(resp.msg);
        setSourceModalOpen(false);
        await getCollectList();
        return;
      }
      message.error(resp.msg || "保存站点失败");
    } finally {
      setSubmitting(false);
    }
  };

  const testApi = async () => {
    try {
      const values = await sourceForm.validateFields();
      const resp = await ApiPost("/manage/collect/test", values);
      if (resp.code === 0) {
        message.success(resp.msg);
        return;
      }
      message.error(resp.msg || "接口测试失败");
    } catch {
      // 表单校验失败时不额外提示。
    }
  };

  const openBatchCollect = async () => {
    setBatchOpen(true);
    const resp = await ApiGet("/manage/collect/options");
    if (resp.code === 0) {
      const options = Array.isArray(resp.data)
        ? resp.data.map((item: BatchOption) => ({
            ...item,
            grade: siteList.find((site) => site.id === item.id)?.grade ?? 1,
            state: siteList.find((site) => site.id === item.id)?.state ?? false,
          }))
        : [];
      setBatchOptions(options);
      return;
    }
    message.error(resp.msg || "加载批量采集站点失败");
  };

  const startBatchCollect = async () => {
    if (batchIds.length === 0) {
      message.warning("请至少选择一个站点");
      return;
    }
    const resp = await ApiPost("/manage/spider/start", {
      ids: batchIds,
      time: batchTime,
      batch: true,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      setBatchOpen(false);
      await getCollectingState();
      return;
    }
    message.error(resp.msg || "批量采集启动失败");
  };

  const clearFilms = async () => {
    if (!password) {
      message.error("请输入密钥");
      return;
    }
    const resp = await ApiPost("/manage/spider/clear", { password });
    if (resp.code === 0) {
      message.success(resp.msg);
    } else {
      message.error(resp.msg || "清空数据失败");
    }
    setClearOpen(false);
    setPassword("");
  };

  const submitStopAllTasks = async () => {
    const resp = await ApiPost("/manage/spider/stopAll", {});
    if (resp.code === 0) {
      message.success(resp.msg);
      await getCollectingState();
      return;
    }
    message.error(resp.msg || "终止任务失败");
  };

  const columns: ColumnsType<FilmSource> = [
    {
      title: "站点",
      dataIndex: "name",
      width: 260,
      render: (name: string, record) => {
        const isRunning = activeCollectIds.includes(record.id);
        return (
          <Flex vertical gap={6}>
            <Space size={[8, 4]} wrap>
              <Typography.Text strong>{name}</Typography.Text>
              <Tag
                color={record.grade === 0 ? "gold" : "default"}
                bordered={false}
              >
                {record.grade === 0 ? "主站" : "附属站"}
              </Tag>
              <Tag
                color={record.state ? "success" : "default"}
                bordered={false}
              >
                {record.state ? "已启用" : "已禁用"}
              </Tag>
              {isRunning ? (
                <Tag
                  icon={<LoadingOutlined />}
                  color="processing"
                  bordered={false}
                >
                  采集中
                </Tag>
              ) : null}
            </Space>
            <Typography.Link
              href={record.uri}
              target="_blank"
              rel="noopener noreferrer"
            >
              {record.uri}
            </Typography.Link>
          </Flex>
        );
      },
    },
    {
      title: "图片同步",
      dataIndex: "syncPictures",
      align: "center",
      width: 120,
      render: (value: boolean, record) => (
        <Switch
          checked={value}
          disabled={record.grade === 1}
          checkedChildren="开启"
          unCheckedChildren="关闭"
          onChange={(checked) => {
            updateSiteListItem(record.id, (item) => ({
              ...item,
              syncPictures: checked,
            }));
            void changeSourceState({ ...record, syncPictures: checked });
          }}
        />
      ),
    },
    {
      title: "启用状态",
      dataIndex: "state",
      align: "center",
      width: 120,
      render: (value: boolean, record) => (
        <Switch
          checked={value}
          checkedChildren="启用"
          unCheckedChildren="禁用"
          onChange={(checked) => {
            updateSiteListItem(record.id, (item) => ({
              ...item,
              state: checked,
            }));
            void changeSourceState({ ...record, state: checked });
          }}
        />
      ),
    },
    {
      title: "请求间隔",
      dataIndex: "interval",
      align: "center",
      width: 120,
      render: (value: number) => (
        <Tag bordered={false}>{value > 0 ? `${value} ms` : "无限制"}</Tag>
      ),
    },
    {
      title: "采集时长",
      width: 160,
      render: (_, record) => (
        <Select
          size="small"
          value={record.cd}
          style={{ width: "100%" }}
          options={collectDuration.map((item) => ({
            value: item.time,
            label: item.label,
          }))}
          onChange={(value) => {
            updateSiteListItem(record.id, (item) => ({ ...item, cd: value }));
          }}
        />
      ),
    },
    {
      title: "操作",
      key: "action",
      fixed: "right",
      align: "center",
      width: 180,
      render: (_, record) => {
        const isRunning = activeCollectIds.includes(record.id);
        return (
          <Space size={4}>
            {!isRunning ? (
              <Tooltip title="开始采集">
                <Button
                  type="primary"
                  icon={<PoweroffOutlined />}
                  onClick={() => void startTask(record)}
                />
              </Tooltip>
            ) : (
              <Tooltip title="停止采集">
                <Button
                  danger
                  icon={<PauseOutlined />}
                  onClick={() => void stopTask(record.id)}
                />
              </Tooltip>
            )}
            <Tooltip title="编辑站点">
              <Button
                icon={<EditOutlined />}
                onClick={() => void openEditDialog(record.id)}
              />
            </Tooltip>
            <Popconfirm
              title="确认删除此采集站？"
              onConfirm={() => void delSource(record.id)}
            >
              <Button danger icon={<DeleteOutlined />} />
            </Popconfirm>
          </Space>
        );
      },
    },
  ];

  return (
    <div className={styles.pageBody}>
      <ManagePageHeader
        title="采集站点"
        description="统一管理主站、附属站与采集任务。"
      />

      <div className={styles.layout}>
        <Card
          size="small"
          title="运行概览"
          className={styles.summaryCard}
          styles={{ body: { height: "100%" } }}
        >
          <Row gutter={[16, 16]} className={styles.overviewRow}>
            <Col xs={12} lg={6} className={styles.overviewCol}>
              <div className={styles.overviewStat}>
                <Statistic title="站点总数" value={stats.total} />
              </div>
            </Col>
            <Col xs={12} lg={6} className={styles.overviewCol}>
              <div className={styles.overviewStat}>
                <Statistic title="启用站点" value={stats.enabled} />
              </div>
            </Col>
            <Col xs={12} lg={6} className={styles.overviewCol}>
              <div className={styles.overviewStat}>
                <Statistic title="运行任务" value={stats.running} />
              </div>
            </Col>
            <Col xs={12} lg={6} className={styles.overviewCol}>
              <div className={styles.overviewStat}>
                <Statistic
                  title="主站状态"
                  value={stats.masters}
                  suffix={<Tag color={masterStatus.color}>{masterStatus.text}</Tag>}
                />
              </div>
            </Col>
          </Row>
        </Card>

        <Card
          size="small"
          title="当前主站"
          className={styles.summaryCard}
          styles={{ body: { height: "100%" } }}
          extra={
            masterSite ? (
              <Tag color="gold">已生效</Tag>
            ) : (
              <Tag color="error">未配置</Tag>
            )
          }
        >
          {masterSite ? (
            <Descriptions
              column={1}
              size="small"
              className={styles.masterDescriptions}
            >
              <Descriptions.Item label="站点名称">
                {masterSite.name}
              </Descriptions.Item>
              <Descriptions.Item label="接口地址">
                <Typography.Link
                  href={masterSite.uri}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {masterSite.uri}
                </Typography.Link>
              </Descriptions.Item>
              <Descriptions.Item label="启用状态">
                <Tag
                  color={masterSite.state ? "success" : "default"}
                  bordered={false}
                >
                  {masterSite.state ? "启用中" : "已停用"}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="图片同步">
                <Tag
                  color={masterSite.syncPictures ? "processing" : "default"}
                  bordered={false}
                >
                  {masterSite.syncPictures ? "开启" : "关闭"}
                </Tag>
              </Descriptions.Item>
            </Descriptions>
          ) : (
            <Descriptions column={1} size="small">
              <Descriptions.Item label="状态">
                <Tag color="warning">未配置</Tag>
              </Descriptions.Item>
              <Descriptions.Item label="说明">
                需要先指定一个主站
              </Descriptions.Item>
            </Descriptions>
          )}
        </Card>

        <Table
          rowKey="id"
          size="middle"
          columns={columns}
          dataSource={siteList}
          loading={loading}
          pagination={false}
          scroll={{ x: 980 }}
          className={styles.tableBlock}
          title={() => (
            <div className={styles.tableToolbar}>
              <div className={styles.tableTitle}>采集站列表</div>
              <Space size={[8, 8]} wrap>
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={openAddDialog}
                >
                  新增站点
                </Button>
                <Button
                  icon={<SendOutlined />}
                  onClick={() => void openBatchCollect()}
                >
                  批量采集
                </Button>
                <Popconfirm
                  title="一键终止所有采集"
                  description="确定要强制终止当前所有正在运行的采集任务吗？"
                  onConfirm={() => void submitStopAllTasks()}
                  okText="确认终止"
                  cancelText="取消"
                  okButtonProps={{ danger: true }}
                  disabled={activeCollectIds.length === 0}
                >
                  <Button
                    danger
                    icon={<PauseOutlined />}
                    disabled={activeCollectIds.length === 0}
                  >
                    终止全部任务
                  </Button>
                </Popconfirm>
                <Button
                  danger
                  icon={<DeleteOutlined />}
                  onClick={() => setClearOpen(true)}
                >
                  清空影片数据
                </Button>
              </Space>
            </div>
          )}
        />
      </div>

      <SourceFormModal
        open={sourceModalOpen}
        mode={sourceModalMode}
        loading={submitting}
        form={sourceForm}
        onCancel={() => setSourceModalOpen(false)}
        onSubmit={handleSubmitSource}
        onTest={testApi}
      />

      <BatchCollectModal
        open={batchOpen}
        options={batchOptions}
        selectedIds={batchIds}
        activeCollectIds={activeCollectIds}
        batchTime={batchTime}
        onCancel={() => setBatchOpen(false)}
        onSubmit={() => void startBatchCollect()}
        onSelectionChange={setBatchIds}
        onBatchTimeChange={setBatchTime}
      />

      <Modal
        title="清空影片数据"
        open={clearOpen}
        onCancel={() => setClearOpen(false)}
        onOk={() => void clearFilms()}
        okText="确认清空"
        okButtonProps={{ danger: true }}
        destroyOnHidden
      >
        <Flex vertical gap={12}>
          <Alert
            showIcon
            type="error"
            message="该操作不可逆"
            description="会清空数据库中的全部影片数据，请确认当前没有误操作风险。"
          />
          <Input.Password
            placeholder="请输入管理密码"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </Flex>
      </Modal>
    </div>
  );
}
