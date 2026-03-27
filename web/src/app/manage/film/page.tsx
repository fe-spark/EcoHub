"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import {
  Table,
  Tag,
  Button,
  Space,
  Select,
  Input,
  DatePicker,
  Popconfirm,
  Tooltip,
  Pagination,
  Card,
  Typography,
} from "antd";
import { useRouter } from "next/navigation";
import {
  SearchOutlined,
  ReloadOutlined,
  EditOutlined,
  DeleteOutlined,
  AimOutlined,
  FireOutlined,
  PlusOutlined,
  FilterOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { ApiGet, ApiPost } from "@/lib/api";
import dayjs from "dayjs";
import styles from "./page.module.less";
import { useAppMessage } from "@/lib/useAppMessage";

const { RangePicker } = DatePicker;
const { Text, Title } = Typography;

interface FilmItem {
  mid: number;
  ID: number;
  name: string;
  cName: string;
  year: string | number;
  score: string | number;
  hits: number;
  remarks: string;
  updateStamp: number;
}

export default function FilmListPage() {
  const router = useRouter();
  const [list, setList] = useState<FilmItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [syncingIds, setSyncingIds] = useState<number[]>([]);
  const [page, setPage] = useState({ current: 1, pageSize: 10, total: 0 });

  // Filters
  const [params, setParams] = useState<any>({
    name: "",
    pid: 0,
    cid: 0,
    plot: "",
    area: "",
    language: "",
    year: "",
    remarks: "",
    beginTime: "",
    endTime: "",
  });

  const [options, setOptions] = useState<any>({
    class: [],
    Plot: [],
    Area: [],
    Language: [],
    year: [],
    remarks: [],
    tags: {},
  });

  const [classId, setClassId] = useState<number>(0);
  const [dateRange, setDateRange] = useState<any>(null);

  const { message } = useAppMessage();

  const getFilmPage = useCallback(
    async (p?: any) => {
      setLoading(true);
      const pg = p || page;
      try {
        const resp = await ApiGet("/manage/film/search/list", {
          ...params,
          current: pg.current,
          pageSize: pg.pageSize,
        });
        if (resp.code === 0) {
          const formattedList = resp.data?.list?.map((item: any) => ({
            ...item,
            year: item.year <= 0 ? "未知" : item.year,
            score: item.score === 0 ? "暂无" : item.score,
          }));
          setList(formattedList);
          setPage(resp.data.params.paging);

          if (resp.data.options) {
            setOptions((prev: any) => ({
              ...prev,
              ...resp.data.options,
            }));
          }
        }
      } finally {
        setLoading(false);
      }
    },
    [params, page],
  );

  useEffect(() => {
    getFilmPage();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleClassChange = (value: number) => {
    setClassId(value);
    const selectedClass = options.class?.find((c: any) => c.id === value);

    const newParams = { ...params };
    if (!selectedClass) {
      newParams.pid = 0;
      newParams.cid = 0;
      setOptions((prev: any) => ({
        ...prev,
        Plot: [],
        Area: [],
        Language: [],
      }));
    } else {
      if (selectedClass.pid <= 0) {
        newParams.pid = selectedClass.id;
        newParams.cid = 0;
      } else {
        newParams.pid = selectedClass.pid;
        newParams.cid = selectedClass.id;
      }

      const t =
        selectedClass.pid === 0
          ? options.tags[selectedClass.id]
          : options.tags[selectedClass.pid];
      setOptions((prev: any) => ({
        ...prev,
        Plot: t?.Plot || [],
        Area: t?.Area || [],
        Language: t?.Language || [],
      }));
    }

    newParams.plot = "";
    newParams.area = "";
    newParams.language = "";
    setParams(newParams);
  };

  const onSearch = () => {
    const p = { ...params };
    if (dateRange?.[0] && dateRange?.[1]) {
      p.beginTime = dateRange[0].format("YYYY-MM-DD HH:mm:ss");
      p.endTime = dateRange[1].format("YYYY-MM-DD HH:mm:ss");
    } else {
      p.beginTime = "";
      p.endTime = "";
    }
    setParams(p);
    setPage({ ...page, current: 1 });
    getFilmPage({ ...page, current: 1 });
  };

  const handleUpdateSingle = useCallback(
    async (mid: number) => {
      setSyncingIds((prev) => [...prev, mid]);
      try {
        const resp = await ApiPost("/manage/spider/update/single", {
          ids: String(mid),
        });
        if (resp.code === 0) {
          message.success(resp.msg);
          getFilmPage();
        } else {
          message.error(resp.msg);
        }
      } finally {
        setTimeout(() => {
          setSyncingIds((prev) => prev.filter((id) => id !== mid));
        }, 500);
      }
    },
    [getFilmPage, message],
  );

  const handleDelFilm = useCallback(
    async (id: number) => {
      const resp = await ApiPost("/manage/film/search/del", { id: String(id) });
      if (resp.code === 0) {
        message.success(resp.msg);
        getFilmPage();
      } else {
        message.error(resp.msg);
      }
    },
    [getFilmPage, message],
  );

  const columns = useMemo<ColumnsType<FilmItem>>(
    () => [
      {
        title: "ID",
        dataIndex: "mid",
        key: "mid",
        width: 80,
        align: "center",
        render: (v) => (
          <Tag color="#8b40ff" style={{ borderRadius: 4 }}>
            #{v}
          </Tag>
        ),
      },
      {
        title: "影片信息",
        key: "info",
        render: (_, record) => (
          <Space size={6} wrap={false}>
            <Text
              className={styles.filmName}
              style={{ whiteSpace: "nowrap" }}
              onClick={() =>
                window.open(`/filmDetail?link=${record.mid}`, "_blank")
              }
            >
              {record.name}
            </Text>
            <Tag color="orange" style={{ borderRadius: 4, flexShrink: 0 }}>
              {record.cName}
            </Tag>
          </Space>
        ),
      },
      {
        title: "评分",
        dataIndex: "score",
        key: "score",
        width: 70,
        align: "center",
        render: (v) => (
          <Text strong style={{ color: "var(--ant-color-primary)" }}>
            {v}
          </Text>
        ),
      },
      {
        title: "年份",
        dataIndex: "year",
        key: "year",
        width: 70,
        align: "center",
        render: (v) => <Text>{v}</Text>,
      },
      {
        title: "热度",
        dataIndex: "hits",
        key: "hits",
        width: 80,
        align: "center",
        render: (v) => (
          <Text type="danger">
            <FireOutlined /> {v}
          </Text>
        ),
      },
      {
        title: "更新状态",
        key: "status",
        align: "center",
        render: (_, record) => (
          <Tag
            color={record.remarks.includes("更新") ? "warning" : "success"}
            style={{ borderRadius: 6, padding: "2px 8px" }}
          >
            {record.remarks}
          </Tag>
        ),
      },
      {
        title: "更新时间",
        dataIndex: "updateStamp",
        align: "center",
        render: (v) => (
          <Text type="secondary" style={{ fontSize: 13 }}>
            {dayjs(v * 1000).format("YYYY-MM-DD HH:ss")}
          </Text>
        ),
      },
      {
        title: "操作",
        key: "action",
        align: "center",
        width: 200,
        fixed: "right",
        render: (_, record) => (
          <Space size={8}>
            <Tooltip title="详情预览">
              <Button
                type="primary"
                shape="circle"
                size="small"
                icon={<AimOutlined />}
                onClick={() =>
                  window.open(`/filmDetail?link=${record.mid}`, "_blank")
                }
              />
            </Tooltip>
            <Tooltip title="同步更新">
              <Button
                type="primary"
                shape="circle"
                size="small"
                style={{ background: "#52c41a", borderColor: "#52c41a" }}
                icon={
                  <ReloadOutlined
                    className={`${styles.syncIcon} ${syncingIds.includes(record.mid) ? styles.syncing : ""}`}
                  />
                }
                onClick={() => handleUpdateSingle(record.mid)}
              />
            </Tooltip>
            <Tooltip title="修改影视">
              <Button
                type="primary"
                shape="circle"
                size="small"
                style={{ background: "#1890ff", borderColor: "#1890ff" }}
                icon={<EditOutlined />}
                onClick={() => router.push(`/manage/film/add?id=${record.mid}`)}
              />
            </Tooltip>
            <Popconfirm
              title="确认删除此影片？"
              onConfirm={() => handleDelFilm(record.ID)}
            >
              <Tooltip title="删除">
                <Button
                  type="primary"
                  danger
                  shape="circle"
                  size="small"
                  icon={<DeleteOutlined />}
                />
              </Tooltip>
            </Popconfirm>
          </Space>
        ),
      },
    ],
    [syncingIds, router, handleDelFilm, handleUpdateSingle],
  );

  return (
    <div className={styles.container}>
      <div className={styles.filterBar}>
        <Input.Search
          placeholder="搜索片名..."
          value={params.name}
          onChange={(e) => setParams({ ...params, name: e.target.value })}
          className={styles.searchInput}
          allowClear
          onSearch={onSearch}
          enterButton
        />
        <Select
          placeholder="选择分类"
          className={styles.filterItem}
          value={classId || undefined}
          onChange={handleClassChange}
          options={options.class?.map((c: any) => ({
            label: c.name,
            value: c.id,
          }))}
          allowClear
        />
        <Select
          placeholder="剧情标签"
          className={styles.filterItem}
          value={params.plot || undefined}
          onChange={(v) => setParams({ ...params, plot: v })}
          options={options.Plot?.map((i: any) => ({
            label: i.Name,
            value: i.Value,
          }))}
          allowClear
        />
        <Select
          placeholder="地区"
          className={styles.filterItem}
          value={params.area || undefined}
          onChange={(v) => setParams({ ...params, area: v })}
          options={options.Area?.map((i: any) => ({
            label: i.Name,
            value: i.Value,
          }))}
          allowClear
        />
        <RangePicker
          showTime
          value={dateRange}
          onChange={(v) => setDateRange(v)}
        />
        <Button type="primary" icon={<SearchOutlined />} onClick={onSearch}>
          搜索
        </Button>
      </div>

      <div className={styles.tableContainer}>
        <Table
          title={() => (
            <div className={styles.tableTitleWarp}>
              <Title level={5} style={{ margin: 0 }}>
                影视资源库
              </Title>
              <Button
                type="primary"
                icon={<PlusOutlined />}
                onClick={() => router.push("/manage/film/add")}
              >
                发布新影片
              </Button>
            </div>
          )}
          columns={columns}
          dataSource={list}
          rowKey="mid"
          loading={loading}
          pagination={false}
          scroll={{ x: "max-content" }}
          size="middle"
          bordered
        />
        <div className={styles.paginationContainer}>
          <Pagination
            current={page.current}
            pageSize={page.pageSize}
            total={page.total}
            showSizeChanger
            showTotal={(total) => `共 ${total} 条`}
            onChange={(current, pageSize) => {
              const newPage = { ...page, current, pageSize };
              setPage(newPage);
              getFilmPage(newPage);
            }}
          />
        </div>
      </div>
    </div>
  );
}
