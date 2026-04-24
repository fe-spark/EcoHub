"use client";

import React, {
  useState,
  useEffect,
  useCallback,
  useRef,
  useMemo,
} from "react";
import {
  Table,
  Tag,
  Switch,
  Select,
  Button,
  Space,
  Modal,
  Input,
  Form,
  Popconfirm,
  Checkbox,
  InputNumber,
  Radio,
  Tooltip,
  Alert,
  Divider,
} from "antd";
import {
  PlusOutlined,
  SendOutlined,
  DeleteOutlined,
  EditOutlined,
  PoweroffOutlined,
  PauseOutlined,
  LoadingOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageShell from "../../components/page-shell";
import styles from "./index.module.less";

interface FilmSource {
  id: string;
  name: string;
  uri: string;
  syncPictures: boolean;
  state: boolean;
  grade: number;
  interval: number;
  cd?: number;
}

const collectDuration = [
  { label: "采集今日", time: 24 },
  { label: "采集三天", time: 72 },
  { label: "采集一周", time: 168 },
  { label: "采集半月", time: 360 },
  { label: "采集一月", time: 720 },
  { label: "采集三月", time: 2160 },
  { label: "采集半年", time: 4320 },
  { label: "全量采集", time: -1 },
];

export default function CollectManagePageView() {
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const [activeCollectIds, setActiveCollectIds] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const timerRef = useRef<NodeJS.Timeout | null>(null);
  const { message } = useAppMessage();

  const [batchOpen, setBatchOpen] = useState(false);
  const [batchIds, setBatchIds] = useState<string[]>([]);
  const [batchTime, setBatchTime] = useState(24);
  const [batchOptions, setBatchOptions] = useState<any[]>([]);

  const [addOpen, setAddOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [form] = Form.useForm();
  const currentGrade = Form.useWatch("grade", form);
  const [editingId, setEditingId] = useState<string | null>(null);

  const [clearOpen, setClearOpen] = useState(false);
  const [password, setPassword] = useState("");

  const enrichedBatchOptions = useMemo(
    () =>
      batchOptions.map((o) => ({
        ...o,
        grade: siteList.find((s) => s.id === o.id)?.grade ?? 1,
        state: siteList.find((s) => s.id === o.id)?.state ?? false,
      })),
    [batchOptions, siteList],
  );

  const enabledBatchIds = useMemo(
    () =>
      enrichedBatchOptions.filter((item) => item.state).map((item) => item.id),
    [enrichedBatchOptions],
  );

  const batchSelectionMap = useMemo(() => new Set(batchIds), [batchIds]);

  const stats = useMemo(
    () => ({
      total: siteList.length,
      enabled: siteList.filter((item) => item.state).length,
      running: activeCollectIds.length,
      masters: siteList.filter((item) => item.grade === 0).length,
    }),
    [siteList, activeCollectIds],
  );

  const selectedBatchSites = useMemo(
    () => enrichedBatchOptions.filter((item) => batchSelectionMap.has(item.id)),
    [enrichedBatchOptions, batchSelectionMap],
  );

  const selectedRunningNames = useMemo(
    () =>
      selectedBatchSites
        .filter((item) => activeCollectIds.includes(item.id))
        .map((item) => item.name),
    [selectedBatchSites, activeCollectIds],
  );

  const batchCheckAll =
    enrichedBatchOptions.length > 0 &&
    batchIds.length === enrichedBatchOptions.length;

  const batchIndeterminate =
    batchIds.length > 0 && batchIds.length < enrichedBatchOptions.length;

  const getCollectList = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/collect/list");
      if (resp.code === 0) {
        const list = resp.data?.map((item: any) => ({
          ...item,
          cd: item.cd || 24,
        }));
        setSiteList(list);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  const getCollectingState = useCallback(async () => {
    const resp = await ApiGet("/manage/collect/collecting/state", undefined);
    if (resp.code === 0 && resp.data) {
      setActiveCollectIds(resp.data);
    }
  }, []);

  useEffect(() => {
    getCollectList();
    getCollectingState();
    timerRef.current = setInterval(() => {
      getCollectingState();
    }, 4000);
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [getCollectList, getCollectingState]);

  useEffect(() => {
    if (currentGrade === 1 && form.getFieldValue("syncPictures")) {
      form.setFieldValue("syncPictures", false);
    }
  }, [currentGrade, form]);

  const changeSourceState = async (record: FilmSource) => {
    const resp = await ApiPost("/manage/collect/change", {
      id: record.id,
      state: record.state,
      syncPictures: record.syncPictures,
    });
    if (resp.code !== 0) message.error(resp.msg);
  };

  const startTask = async (record: FilmSource) => {
    const resp = await ApiPost("/manage/spider/start", {
      id: record.id,
      time: record.cd || 24,
      batch: false,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      getCollectingState();
    } else {
      message.error(resp.msg);
    }
  };

  const stopTask = async (id: string) => {
    const resp = await ApiPost("/manage/collect/stop", { id });
    if (resp.code === 0) {
      message.success("已请求停止任务");
      getCollectingState();
    }
  };

  const delSource = async (id: string) => {
    const resp = await ApiPost("/manage/collect/del", { id });
    if (resp.code === 0) {
      message.success(resp.msg);
      getCollectList();
    } else {
      message.error(resp.msg);
    }
  };

  const openAddDialog = () => {
    form.resetFields();
    form.setFieldsValue({
      grade: 1,
      syncPictures: false,
      state: false,
      interval: 0,
    });
    setAddOpen(true);
  };

  const openEditDialog = async (id: string) => {
    setEditingId(id);
    const resp = await ApiGet("/manage/collect/find", { id });
    if (resp.code === 0) {
      form.setFieldsValue(resp.data);
      setEditOpen(true);
    } else {
      message.error(resp.msg);
    }
  };

  const onAddFinish = async (values: any) => {
    const resp = await ApiPost("/manage/collect/add", values);
    if (resp.code === 0) {
      message.success(resp.msg);
      setAddOpen(false);
      getCollectList();
    } else {
      message.error(resp.msg);
    }
  };

  const onEditFinish = async (values: any) => {
    const resp = await ApiPost("/manage/collect/update", {
      ...values,
      id: editingId,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      setEditOpen(false);
      getCollectList();
    } else {
      message.error(resp.msg);
    }
  };

  const testApi = async () => {
    try {
      const values = await form.validateFields();
      const resp = await ApiPost("/manage/collect/test", values);
      if (resp.code === 0) message.success(resp.msg);
      else message.error(resp.msg);
    } catch {}
  };

  const openBatchCollect = async () => {
    setBatchOpen(true);
    const resp = await ApiGet("/manage/collect/options");
    if (resp.code === 0) setBatchOptions(resp.data || []);
  };

  const selectAllBatchSites = () => {
    setBatchIds(enrichedBatchOptions.map((item) => item.id));
  };

  const clearBatchSelection = () => {
    setBatchIds([]);
  };

  const selectEnabledBatchSites = () => {
    setBatchIds(enabledBatchIds);
  };

  const invertBatchSelection = () => {
    setBatchIds(
      enrichedBatchOptions
        .filter((item) => !batchSelectionMap.has(item.id))
        .map((item) => item.id),
    );
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
      getCollectingState();
    } else {
      message.error(resp.msg);
    }
  };

  const clearFilms = async () => {
    if (!password) {
      message.error("请输入密钥");
      return;
    }
    const resp = await ApiPost("/manage/spider/clear", { password });
    if (resp.code === 0) message.success(resp.msg);
    else message.error(resp.msg);
    setClearOpen(false);
    setPassword("");
  };

  const submitStopAllTasks = async () => {
    const resp = await ApiPost("/manage/spider/stopAll", {});
    if (resp.code === 0) {
      message.success(resp.msg);
      getCollectingState();
    } else {
      message.error(resp.msg);
    }
  };

  const columns: ColumnsType<FilmSource> = [
    {
      title: "资源名称",
      dataIndex: "name",
      key: "name",
      render: (name: string, record) => (
        <Space>
          <span>{name}</span>
          {activeCollectIds.includes(record.id) && (
            <LoadingOutlined style={{ color: "var(--ant-color-primary)" }} />
          )}
        </Space>
      ),
    },
    {
      title: "资源站",
      dataIndex: "uri",
      ellipsis: true,
      render: (uri: string) => (
        <a href={uri} target="_blank" rel="noopener noreferrer">
          {uri}
        </a>
      ),
    },
    {
      title: "同步图片",
      dataIndex: "syncPictures",
      align: "center",
      render: (v: boolean, record) => (
        <Switch
          checked={v}
          disabled={record.grade === 1}
          onChange={(checked) => {
            record.syncPictures = checked;
            setSiteList([...siteList]);
            changeSourceState(record);
          }}
          checkedChildren="开启"
          unCheckedChildren="关闭"
        />
      ),
    },
    {
      title: "是否启用",
      dataIndex: "state",
      align: "center",
      render: (v: boolean, record) => (
        <Switch
          checked={v}
          onChange={(checked) => {
            record.state = checked;
            setSiteList([...siteList]);
            changeSourceState(record);
          }}
          checkedChildren="启用"
          unCheckedChildren="禁用"
        />
      ),
    },
    {
      title: "站点权重",
      dataIndex: "grade",
      align: "center",
      render: (v: number) => (
        <Tag color={v === 0 ? "green" : "default"}>
          {v === 0 ? "采集主站" : "附属站点"}
        </Tag>
      ),
    },
    {
      title: "采集间隔",
      dataIndex: "interval",
      align: "center",
      render: (v: number) => (
        <Tag color="cyan">{v > 0 ? `${v} ms` : "无限制"}</Tag>
      ),
    },
    {
      title: "采集方式",
      width: 120,
      render: (_, record) => (
        <Select
          size="small"
          value={record.cd}
          onChange={(v) => {
            record.cd = v;
            setSiteList([...siteList]);
          }}
          style={{ width: "100%" }}
          options={collectDuration.map((d) => ({
            value: d.time,
            label: d.label,
          }))}
        />
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "center",
      width: 160,
      fixed: "right",
      render: (_, record) => {
        const isRunning = activeCollectIds.includes(record.id);

        const renderStartBtn = () => {
          return isRunning ? null : (
            <Tooltip title="开始采集">
              <Button
                type="primary"
                icon={<PoweroffOutlined />}
                shape="circle"
                size="small"
                style={{
                  background: "var(--ant-color-success)",
                  borderColor: "var(--ant-color-success)",
                }}
                onClick={() => startTask(record)}
              />
            </Tooltip>
          );
        };

        return (
          <Space>
            {renderStartBtn()}
            {isRunning && (
              <Tooltip title="停止采集">
                <Button
                  type="primary"
                  danger
                  icon={<PauseOutlined />}
                  shape="circle"
                  size="small"
                  onClick={() => stopTask(record.id)}
                />
              </Tooltip>
            )}
            <Button
              type="primary"
              icon={<EditOutlined />}
              shape="circle"
              size="small"
              onClick={() => openEditDialog(record.id)}
            />
            <Popconfirm
              title="确认删除此采集站？"
              onConfirm={() => delSource(record.id)}
            >
              <Button
                danger
                icon={<DeleteOutlined />}
                shape="circle"
                size="small"
              />
            </Popconfirm>
          </Space>
        );
      },
    },
  ];

  const commonFormItems = (
    <>
      <Form.Item label="资源名称" name="name" rules={[{ required: true }]}>
        <Input placeholder="自定义资源名称" />
      </Form.Item>
      <Form.Item label="接口地址" name="uri" rules={[{ required: true }]}>
        <Input placeholder="资源采集链接" />
      </Form.Item>
      <Form.Item
        label="间隔时长"
        name="interval"
        tooltip="单次请求的时间间隔, 单位/ms"
      >
        <InputNumber min={0} step={100} style={{ width: "100%" }} />
      </Form.Item>
      <Form.Item label="站点类型" name="grade">
        <Radio.Group
          onChange={(e) => {
            if (e.target.value === 1) form.setFieldValue("syncPictures", false);
          }}
        >
          <Radio value={0}>主站点</Radio>
          <Radio value={1}>附属站点</Radio>
        </Radio.Group>
      </Form.Item>
      <Form.Item label="图片同步" name="syncPictures" valuePropName="checked">
        <Switch
          checkedChildren="开启"
          unCheckedChildren="关闭"
          disabled={currentGrade === 1}
        />
      </Form.Item>
      <Form.Item label="是否启用" name="state" valuePropName="checked">
        <Switch checkedChildren="启用" unCheckedChildren="禁用" />
      </Form.Item>
    </>
  );

  return (
    <ManagePageShell
      eyebrow="采集中心"
      title="采集站点"
      description="统一管理主站、附属站和批量采集流程，随时查看站点状态与任务运行情况。"
      actions={
        <div className={styles.heroActions}>
          <Button type="primary" icon={<PlusOutlined />} onClick={openAddDialog}>
            添加采集站
          </Button>
          <Button
            type="primary"
            icon={<SendOutlined />}
            className={styles.successButton}
            onClick={openBatchCollect}
          >
            一键采集
          </Button>
          <Popconfirm
            title="一键终止所有采集"
            description="确定要强制终止当前所有正在运行的采集任务吗？这可能导致部分数据不完整。"
            onConfirm={submitStopAllTasks}
            okText="确认终止"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            disabled={activeCollectIds.length === 0}
          >
            <Button
              type="primary"
              danger
              icon={<PauseOutlined />}
              disabled={activeCollectIds.length === 0}
            >
              一键终止
            </Button>
          </Popconfirm>
          <Button danger icon={<DeleteOutlined />} onClick={() => setClearOpen(true)}>
            清空数据
          </Button>
        </div>
      }
      extra={
        <div className={styles.statsGrid}>
          <div className={styles.statCard}>
            <span className={styles.statLabel}>站点总数</span>
            <strong className={styles.statValue}>{stats.total}</strong>
            <span className={styles.statHint}>当前已登记的全部采集源</span>
          </div>
          <div className={styles.statCard}>
            <span className={styles.statLabel}>启用站点</span>
            <strong className={styles.statValue}>{stats.enabled}</strong>
            <span className={styles.statHint}>可参与日常采集的站点</span>
          </div>
          <div className={styles.statCard}>
            <span className={styles.statLabel}>运行中任务</span>
            <strong className={styles.statValue}>{stats.running}</strong>
            <span className={styles.statHint}>重复发起会被后端直接跳过</span>
          </div>
          <div className={styles.statCard}>
            <span className={styles.statLabel}>主站数量</span>
            <strong className={styles.statValue}>{stats.masters}</strong>
            <span className={styles.statHint}>主站会优先承担分类映射同步</span>
          </div>
        </div>
      }
      panelClassName={styles.tablePanel}
    >
      <section className={styles.tableSection}>
        <div className={styles.sectionHeader}>
          <div>
            <h3 className={styles.sectionTitle}>采集站列表</h3>
            <p className={styles.sectionDesc}>
              可逐站控制采集方式、启用状态与运行操作。
            </p>
          </div>
        </div>
        <Table
          columns={columns}
          dataSource={siteList}
          rowKey="id"
          loading={loading}
          bordered
          size="middle"
          pagination={false}
          scroll={{ x: "max-content" }}
        />
      </section>

      <Modal
        title="添加采集站点"
        open={addOpen}
        onCancel={() => setAddOpen(false)}
        onOk={() => form.submit()}
        footer={[
          <Button key="test" type="dashed" onClick={testApi}>
            测试接口
          </Button>,
          <Button key="cancel" onClick={() => setAddOpen(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" onClick={() => form.submit()}>
            添加
          </Button>,
        ]}
      >
        <Form form={form} labelCol={{ span: 6 }} onFinish={onAddFinish}>
          {commonFormItems}
        </Form>
      </Modal>

      <Modal
        title="修改分类信息"
        open={editOpen}
        onCancel={() => setEditOpen(false)}
        onOk={() => form.submit()}
        footer={[
          <Button key="test" type="dashed" onClick={testApi}>
            测试接口
          </Button>,
          <Button key="cancel" onClick={() => setEditOpen(false)}>
            取消
          </Button>,
          <Button key="ok" type="primary" onClick={() => form.submit()}>
            更新
          </Button>,
        ]}
      >
        <Form form={form} labelCol={{ span: 6 }} onFinish={onEditFinish}>
          {commonFormItems}
        </Form>
      </Modal>

      <Modal
        title="多资源站一键采集"
        open={batchOpen}
        onCancel={() => setBatchOpen(false)}
        onOk={startBatchCollect}
        okText="确认执行"
        width={840}
      >
        {selectedRunningNames.length > 0 && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            message="以下站点正在采集中"
            description={`${selectedRunningNames.join("、")}；重复启动会被跳过，不会中断当前任务。`}
          />
        )}

        <div className={styles.batchPanel}>
          <div className={styles.batchPanelHeader}>
            <div>
              <div className={styles.sectionTitle}>选择执行站点</div>
              <p className={styles.sectionDesc}>
                支持全选、仅选启用站点和反选，适合快速发起大批量采集。
              </p>
            </div>
            <div className={styles.batchMeta}>
              <span>已选 {batchIds.length} 个</span>
              <span>启用 {enabledBatchIds.length} 个</span>
              <span>运行中 {activeCollectIds.length} 个</span>
            </div>
          </div>

          <div className={styles.batchToolbar}>
            <Checkbox
              checked={batchCheckAll}
              indeterminate={batchIndeterminate}
              onChange={(e) => {
                if (e.target.checked) {
                  selectAllBatchSites();
                  return;
                }
                clearBatchSelection();
              }}
            >
              全选站点
            </Checkbox>
            <Space wrap>
              <Button size="small" onClick={selectEnabledBatchSites}>
                仅选启用站点
              </Button>
              <Button size="small" onClick={invertBatchSelection}>
                反选
              </Button>
              <Button size="small" onClick={clearBatchSelection}>
                清空选择
              </Button>
            </Space>
          </div>

          <Checkbox.Group
            value={batchIds}
            onChange={(v) => setBatchIds(v as string[])}
          >
            <div className={styles.batchSiteGrid}>
              {enrichedBatchOptions.map((o) => {
                const isRunning = activeCollectIds.includes(o.id);
                const isChecked = batchSelectionMap.has(o.id);
                return (
                  <label
                    key={o.id}
                    className={`${styles.batchSiteCard} ${isChecked ? styles.batchSiteCardSelected : ""}`}
                  >
                    <div className={styles.batchSiteMain}>
                      <Checkbox value={o.id}>{o.name}</Checkbox>
                      <div className={styles.batchSiteTags}>
                        <Tag
                          color={o.grade === 0 ? "green" : "default"}
                          style={{ marginRight: 0 }}
                        >
                          {o.grade === 0 ? "主站" : "附属站"}
                        </Tag>
                        <Tag
                          color={o.state ? "success" : "error"}
                          style={{ marginRight: 0 }}
                        >
                          {o.state ? "已启用" : "已停用"}
                        </Tag>
                        {isRunning && (
                          <Tag
                            color="processing"
                            icon={<LoadingOutlined />}
                            style={{ marginRight: 0 }}
                          >
                            采集中
                          </Tag>
                        )}
                      </div>
                    </div>
                    <div className={styles.batchSiteSub}>{o.id}</div>
                  </label>
                );
              })}
            </div>
          </Checkbox.Group>

          <Divider style={{ margin: "20px 0 16px" }} />

          <Form layout="vertical">
            <Form.Item label="采集时长" style={{ marginBottom: 0 }}>
              <Select
                value={batchTime}
                onChange={setBatchTime}
                options={collectDuration.map((d) => ({
                  label: d.label,
                  value: d.time,
                }))}
              />
            </Form.Item>
          </Form>
        </div>
      </Modal>

      <Modal
        title="清空影视数据"
        open={clearOpen}
        onCancel={() => setClearOpen(false)}
        onOk={clearFilms}
        okText="确认执行"
        okButtonProps={{ danger: true }}
      >
        <p style={{ color: "var(--ant-color-error)", marginBottom: 16 }}>
          此操作不可逆，将清空数据库中所有影片信息！
        </p>
        <Input.Password
          placeholder="请输入管理密码"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </Modal>
    </ManagePageShell>
  );
}
