"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { Table, Pagination } from "antd";
import { useRouter } from "next/navigation";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import { useFilmColumns, type FilmItem } from "./film-columns";
import { FilmFilterBar } from "./film-filter-bar";
import { FilmBatchBar } from "./film-batch-bar";
import styles from "./index.module.less";

function flattenCategoryNodes(nodes: any[]): any[] {
  const result: any[] = [];
  const walk = (list: any[]) => {
    for (const item of list) {
      result.push(item);
      if (item.children && item.children.length > 0) {
        walk(item.children);
      }
    }
  };
  walk(nodes || []);
  return result;
}

export default function FilmListPageView() {
  const router = useRouter();
  const { canWrite } = useManagePermission();
  const [list, setList] = useState<FilmItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [syncingIds, setSyncingIds] = useState<number[]>([]);
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [updating, setUpdating] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [page, setPage] = useState({ current: 1, pageSize: 10, total: 0 });
  const [params, setParams] = useState<any>({
    name: "",
    sourceId: "",
    pid: 0,
    cid: 0,
    beginTime: "",
    endTime: "",
  });
  const [tableSourceId, setTableSourceId] = useState<string>("");
  const [options, setOptions] = useState<any>({
    sources: [],
    defaultSourceId: "",
    currentSourceId: "",
    class: [],
    categoryTree: [],
  });
  const [classId, setClassId] = useState<number>(0);
  const [categoryLoading, setCategoryLoading] = useState(false);
  const [treeExpandedKeys, setTreeExpandedKeys] = useState<(string | number)[]>([]);
  const sourceReqSeqRef = useRef(0);
  const [dateRange, setDateRange] = useState<any>(null);
  const { message } = useAppMessage();

  useEffect(() => {
    const collectParentKeys = (nodes: any[]): (string | number)[] => {
      const keys: (string | number)[] = [];
      const walk = (list: any[]) => {
        for (const item of list) {
          if (item.children && item.children.length > 0) {
            keys.push(String(item.id));
            walk(item.children);
          }
        }
      };
      walk(options.categoryTree || []);
      return keys;
    };
    setTreeExpandedKeys(collectParentKeys(options.categoryTree || []));
  }, [options.categoryTree]);

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
        if (resp.code !== 0) {
          message.error(resp.msg || "影片列表获取失败");
          return;
        }
        const formattedList = resp.data?.list?.map((item: any) => ({
          ...item,
          year: item.year <= 0 ? "未知" : item.year,
          score: item.score === 0 ? "暂无" : item.score,
        }));
        setList(formattedList);
        setPage(resp.data.params.paging);
        const activeSource = reqParams.sourceId || resp.data.params?.sourceId || "";
        if (activeSource) {
          setTableSourceId(activeSource);
          if (!reqParams.sourceId) {
            setParams((prev: any) => ({ ...prev, sourceId: activeSource }));
          }
        }
      } catch {
        message.error("影片列表获取失败");
      } finally {
        setLoading(false);
      }
    },
    [message, params, page],
  );

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setCategoryLoading(true);
      try {
        const resp = await ApiGet("/manage/film/search/options", { sourceId: "" });
        const nextParams = {
          name: "",
          sourceId: "",
          pid: 0,
          cid: 0,
          beginTime: "",
          endTime: "",
        };
        if (cancelled) return;
        if (resp.code !== 0 || !resp.data) {
          message.error(resp.msg || "筛选项获取失败");
          return;
        }
        setOptions(resp.data);
        nextParams.sourceId = resp.data.currentSourceId || resp.data.defaultSourceId || "";
        setParams(nextParams);
        setTableSourceId(nextParams.sourceId);
        await getFilmPage({ current: 1, pageSize: page.pageSize || 10, total: 0 }, nextParams);
      } finally {
        if (!cancelled) setCategoryLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleSourceChange = async (value?: string) => {
    const nextSourceId = value || "";
    setParams((prev: any) => ({
      ...prev,
      sourceId: nextSourceId,
      pid: 0,
      cid: 0,
    }));
    setClassId(0);
    setCategoryLoading(true);
    const seq = ++sourceReqSeqRef.current;

    try {
      const resp = await ApiGet("/manage/film/search/options", {
        sourceId: nextSourceId,
      });
      if (seq !== sourceReqSeqRef.current) return;
      if (resp.code !== 0 || !resp.data) {
        message.error(resp.msg || "筛选项获取失败");
        return;
      }
      setOptions((prev: any) => ({
        ...prev,
        ...resp.data,
      }));
    } catch {
      if (seq === sourceReqSeqRef.current) message.error("筛选项获取失败");
    } finally {
      if (seq === sourceReqSeqRef.current) {
        setCategoryLoading(false);
      }
    }
  };

  const handleClassChange = (value?: number) => {
    const val = value || 0;
    setClassId(val);
    const selectedClass = options.class?.find((c: any) => c.id === val);
    setParams((prev: any) => {
      const next = { ...prev };
      if (!selectedClass || val === 0) {
        next.pid = 0;
        next.cid = 0;
      } else if (selectedClass.pid <= 0) {
        next.pid = selectedClass.id;
        next.cid = 0;
      } else {
        next.pid = selectedClass.pid;
        next.cid = selectedClass.id;
      }
      return next;
    });
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
    const defaultSrc =
      options.defaultSourceId ||
      options.sources?.[0]?.id ||
      "";
    const emptyParams = {
      name: "",
      sourceId: defaultSrc,
      pid: 0,
      cid: 0,
      beginTime: "",
      endTime: "",
    };
    setParams(emptyParams);
    setClassId(0);
    setDateRange(null);
    const newPage = { ...page, current: 1 };
    setPage(newPage);
    void getFilmPage(newPage, emptyParams);
  };

  const handleUpdateFilms = useCallback(
    async (ids: number[]) => {
      if (!ids.length) return;
      setUpdating(true);
      if (ids.length === 1) {
        setSyncingIds((prev) => [...prev, ids[0]]);
      }
      try {
        const resp = await ApiPost("/manage/spider/update", { ids });
        if (resp.code === 0) {
          message.success(resp.msg || "更新任务已开启");
          getFilmPage();
        } else {
          message.error(resp.msg || "更新失败");
        }
      } finally {
        setUpdating(false);
        if (ids.length === 1) {
          setTimeout(() => {
            setSyncingIds((prev) => prev.filter((id) => id !== ids[0]));
          }, 500);
        }
      }
    },
    [getFilmPage, message],
  );

  const handleDelFilms = useCallback(
    async (ids: number[]) => {
      if (!ids.length) return;
      setDeleting(true);
      try {
        const resp = await ApiPost("/manage/film/search/del", { ids });
        if (resp.code === 0) {
          message.success(resp.msg || "影片删除成功");
          setSelectedRowKeys((prev) => prev.filter((k) => !ids.includes(Number(k))));
          getFilmPage();
        } else {
          message.error(resp.msg || "删除失败");
        }
      } finally {
        setDeleting(false);
      }
    },
    [getFilmPage, message],
  );

  const columns = useFilmColumns({
    syncingIds,
    canWrite,
    router,
    styles,
    handleUpdateSingle: (mid) => handleUpdateFilms([mid]),
    handleDelFilm: (id) => handleDelFilms([id]),
    activeSourceId: tableSourceId || params.sourceId,
  });

  const categoryTreeData = useMemo(() => {
    const formatNodes = (nodes: any[]): any[] => {
      if (!nodes || nodes.length === 0) return [];
      return nodes.map((node) => {
        const hasChildren = node.children && node.children.length > 0;
        const item: any = {
          title: node.name,
          value: node.id,
          key: String(node.id),
        };
        if (hasChildren) {
          item.children = formatNodes(node.children);
        }
        return item;
      });
    };
    return formatNodes(options.categoryTree || []);
  }, [options.categoryTree]);

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader title="影片列表" />

      <FilmFilterBar
        params={params}
        setParams={setParams}
        options={options}
        classId={classId}
        dateRange={dateRange}
        setDateRange={setDateRange}
        handleSourceChange={handleSourceChange}
        handleClassChange={handleClassChange}
        onSearch={onSearch}
        onReset={onReset}
        categoryTreeData={categoryTreeData}
        categoryLoading={categoryLoading}
        treeExpandedKeys={treeExpandedKeys}
        onTreeExpand={setTreeExpandedKeys}
        styles={styles}
      />

      <Table
        columns={columns}
        dataSource={list}
        rowKey="mid"
        loading={loading}
        pagination={false}
        scroll={{ x: "max-content" }}
        size="middle"
        rowSelection={{
          selectedRowKeys,
          onChange: (keys) => setSelectedRowKeys(keys),
        }}
        title={() => (
          <FilmBatchBar
            selectedCount={selectedRowKeys.length}
            canWrite={canWrite}
            updating={updating}
            deleting={deleting}
            onBatchUpdate={() => handleUpdateFilms(selectedRowKeys.map(Number))}
            onBatchDelete={() => handleDelFilms(selectedRowKeys.map(Number))}
            onClearSelection={() => setSelectedRowKeys([])}
            onAdd={() => {
              const query = new URLSearchParams();
              const targetSourceId =
                params.sourceId ||
                tableSourceId ||
                options.defaultSourceId ||
                options.sources?.[0]?.id;
              const targetCid = classId || params.cid || params.pid;
              if (targetSourceId) query.set("sourceId", targetSourceId);
              if (targetCid) query.set("cid", String(targetCid));
              const qs = query.toString();
              router.push(qs ? `/manage/film/add?${qs}` : "/manage/film/add");
            }}
            styles={styles}
          />
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
    </div>
  );
}
