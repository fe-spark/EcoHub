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
  Typography,
  Card,
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
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import dayjs from "dayjs";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import { useFilmColumns, type FilmItem } from "./film-columns";
import TmdbModal from "../components/tmdb-modal";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import styles from "./index.module.less";

const { RangePicker } = DatePicker;
const { Text } = Typography;

export default function FilmListPageView() {
  const router = useRouter();
  const { canWrite } = useManagePermission();
  const tmdbEnabled = useTmdbEnabled();
  const [list, setList] = useState<FilmItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [syncingIds, setSyncingIds] = useState<number[]>([]);
  const [scrapeModalOpen, setScrapeModalOpen] = useState(false);
  const [currentScrapeFilm, setCurrentScrapeFilm] = useState<FilmItem | null>(null);
  const [page, setPage] = useState({ current: 1, pageSize: 10, total: 0 });
  const [params, setParams] = useState<any>({
    name: "",
    pid: 0,
    cid: 0,
    plot: "",
    area: "",
    language: "",
    year: "",
    beginTime: "",
    endTime: "",
  });
  const [options, setOptions] = useState<any>({
    class: [],
    Plot: [],
    Area: [],
    Language: [],
    year: [],
    tags: {},
  });
  const [classId, setClassId] = useState<number>(0);
  const [dateRange, setDateRange] = useState<any>(null);
  const { message } = useAppMessage();

  const getFilmPage = useCallback(
    async (p?: any, overrideParams?: any) => {
      setLoading(true);
      const pg = p || page;
      const reqParams = overrideParams || params;
      try {
        const resp = await ApiGet("/manage/film/search/list", {
          ...reqParams,
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
    const newPage = { ...page, current: 1 };
    setPage(newPage);
    void getFilmPage(newPage, p);
  };

  const onReset = () => {
    const emptyParams = {
      name: "",
      pid: 0,
      cid: 0,
      plot: "",
      area: "",
      language: "",
      year: "",
      beginTime: "",
      endTime: "",
    };
    setParams(emptyParams);
    setClassId(0);
    setDateRange(null);
    setOptions((prev: any) => ({
      ...prev,
      Plot: [],
      Area: [],
      Language: [],
    }));
    const newPage = { ...page, current: 1 };
    setPage(newPage);
    void getFilmPage(newPage, emptyParams);
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

  const handleScrapeFilm = useCallback((record: FilmItem) => {
    setCurrentScrapeFilm(record);
    setScrapeModalOpen(true);
  }, []);

  const columns = useFilmColumns({
    syncingIds,
    canWrite,
    router,
    styles,
    handleUpdateSingle,
    handleDelFilm,
    handleScrapeFilm,
    tmdbEnabled,
  });

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader
        title="影片列表"
        description="管理当前主库存影片，支持分类、剧情、地区和时间范围筛选。"
      />

      <Space size={[8, 8]} wrap className={styles.filterBar}>
        <Input
          placeholder="搜索片名..."
          value={params.name}
          onChange={(e) => setParams({ ...params, name: e.target.value })}
          className={styles.searchInput}
          allowClear
          onPressEnter={onSearch}
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
          className={styles.dateRange}
        />
        <Button type="primary" icon={<SearchOutlined />} onClick={onSearch} className={styles.searchButton}>
          搜索
        </Button>
        <Button icon={<ReloadOutlined />} onClick={onReset}>
          重置
        </Button>
      </Space>

      <Table
        bordered
        columns={columns}
        dataSource={list}
        rowKey="mid"
        loading={loading}
        pagination={false}
        scroll={{ x: "max-content" }}
        size="middle"
        title={() => (
          <div className={styles.tableHeader}>
            <div className={styles.tableTitle}>影片资源库</div>
            <Space size={[8, 8]} wrap className={styles.tableActions}>
              <Button
                type="primary"
                icon={<PlusOutlined />}
                disabled={!canWrite}
                onClick={() => router.push("/manage/film/add")}
              >
                新增影视
              </Button>
            </Space>
          </div>
        )}
        footer={() => (
          <div className={styles.pagination}>
            <Pagination
              current={page.current}
              pageSize={page.pageSize}
              total={page.total}
              showSizeChanger
              pageSizeOptions={[10, 20, 50, 100, 500]}
              showTotal={(total) => `共 ${total} 条`}
              onChange={(current, pageSize) => {
                const newPage = { ...page, current, pageSize };
                setPage(newPage);
                getFilmPage(newPage);
              }}
            />
          </div>
        )}
      />

      <TmdbModal
        open={scrapeModalOpen}
        mid={currentScrapeFilm ? (currentScrapeFilm.mid || currentScrapeFilm.ID) : undefined}
        initialName={currentScrapeFilm?.name}
        onClose={() => {
          setScrapeModalOpen(false);
          setCurrentScrapeFilm(null);
        }}
        onSuccess={() => getFilmPage()}
      />
    </div>
  );
}
