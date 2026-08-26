"use client";

import React, { useMemo, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Flex,
  Input,
  Modal,
  Space,
  Typography,
  Upload,
} from "antd";
import { DownloadOutlined, UploadOutlined, CloudSyncOutlined } from "@ant-design/icons";
import type { UploadProps } from "antd";
import Link from "next/link";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ResetSiteDataCard from "@/app/manage/components/reset-site-data-card";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

interface ConfigBackupModules {
  site: boolean;
  filmSources: boolean;
  cronTasks: boolean;
  banners: boolean;
  notify: boolean;
  mappingRules: boolean;
}

interface ConfigBackup {
  version: number;
  exportedAt?: string;
  appVersion?: string;
  site?: unknown;
  filmSources?: unknown[];
  cronTasks?: unknown[];
  banners?: unknown[];
  notify?: unknown;
  mappingRules?: unknown[];
}

const MODULE_OPTIONS: { key: keyof ConfigBackupModules; label: string }[] = [
  { key: "site", label: "网站配置" },
  { key: "filmSources", label: "采集站" },
  { key: "cronTasks", label: "计划任务" },
  { key: "banners", label: "首页轮播" },
  { key: "notify", label: "通知配置" },
  { key: "mappingRules", label: "映射规则" },
];

const ALL_MODULES: ConfigBackupModules = {
  site: true,
  filmSources: true,
  cronTasks: true,
  banners: true,
  notify: true,
  mappingRules: true,
};

function downloadJson(filename: string, data: unknown) {
  const blob = new Blob([JSON.stringify(data, null, 2)], {
    type: "application/json;charset=utf-8",
  });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

function summarizeBackup(backup: ConfigBackup | null): string {
  if (!backup) {
    return "";
  }
  const parts: string[] = [];
  if (backup.exportedAt) {
    parts.push(`导出时间 ${backup.exportedAt}`);
  }
  if (backup.appVersion) {
    parts.push(`版本 ${backup.appVersion}`);
  }
  parts.push(`格式 v${backup.version ?? "?"}`);
  if (Array.isArray(backup.filmSources)) {
    parts.push(`采集站 ${backup.filmSources.length}`);
  }
  if (Array.isArray(backup.cronTasks)) {
    parts.push(`计划任务 ${backup.cronTasks.length}`);
  }
  if (Array.isArray(backup.banners)) {
    parts.push(`封面 ${backup.banners.length}`);
  }
  if (Array.isArray(backup.mappingRules)) {
    parts.push(`映射规则 ${backup.mappingRules.length}`);
  }
  return parts.join(" · ");
}

interface DataSecurityPageViewProps {
  /** 嵌入系统设置 Tabs 时隐藏独立页头 */
  embedded?: boolean;
}

/** 数据安全：配置备份导入/导出 + 影视数据重置 */
export default function DataSecurityPageView({ embedded = false }: DataSecurityPageViewProps) {
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();
  const [exporting, setExporting] = useState(false);

  const [importOpen, setImportOpen] = useState(false);
  const [importing, setImporting] = useState(false);
  const [password, setPassword] = useState("");
  const [modules, setModules] = useState<ConfigBackupModules>({ ...ALL_MODULES });
  const [backup, setBackup] = useState<ConfigBackup | null>(null);
  const [fileName, setFileName] = useState("");

  const backupSummary = useMemo(() => summarizeBackup(backup), [backup]);

  const handleExport = async () => {
    setExporting(true);
    try {
      const resp = await ApiGet<ConfigBackup>("/manage/config/backup/export");
      if (resp.code !== 0 || !resp.data) {
        message.error(resp.msg || "导出失败");
        return;
      }
      const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-");
      downloadJson(`ecohub-config-backup-${stamp}.json`, resp.data);
      message.success("配置备份已下载");
    } finally {
      setExporting(false);
    }
  };

  const beforeUpload: UploadProps["beforeUpload"] = (file) => {
    const reader = new FileReader();
    reader.onload = () => {
      try {
        const text = String(reader.result || "");
        const parsed = JSON.parse(text) as ConfigBackup;
        if (!parsed || typeof parsed !== "object") {
          message.error("备份文件格式无效");
          return;
        }
        setBackup(parsed);
        setFileName(file.name);
        message.success(`已读取 ${file.name}`);
      } catch {
        message.error("无法解析 JSON 备份文件");
      }
    };
    reader.onerror = () => message.error("读取文件失败");
    reader.readAsText(file);
    return false;
  };

  const openImport = () => {
    setImportOpen(true);
    setPassword("");
  };

  const closeImport = () => {
    if (importing) {
      return;
    }
    setImportOpen(false);
    setPassword("");
  };

  const confirmImport = async () => {
    if (!backup) {
      message.warning("请先选择备份文件");
      return;
    }
    if (!password.trim()) {
      message.error("请输入管理密码");
      return;
    }
    if (!Object.values(modules).some(Boolean)) {
      message.warning("请至少选择一个导入模块");
      return;
    }
    setImporting(true);
    try {
      const resp = await ApiPost("/manage/config/backup/import", {
        password,
        modules,
        backup,
      });
      if (resp.code === 0) {
        message.success(resp.msg || "配置导入成功");
        setImportOpen(false);
        setPassword("");
        return;
      }
      message.error(resp.msg || "导入失败");
    } finally {
      setImporting(false);
    }
  };

  return (
    <div className={styles.page}>
      {embedded ? null : (
        <ManagePageHeader
          title="数据安全"
          description="管理站点配置备份导入与导出，以及影视库存与采集派生数据重置。"
        />
      )}

      {embedded ? null : (
        <Alert
          type="info"
          showIcon
          title="配置备份与数据重置"
          description={
            <>
              导出/导入仅包含站点配置（网站、采集站、计划任务、封面、通知、映射规则），不含影视库存与账号密码。
              当前影视体量可在{" "}
              <Link href="/manage">工作台</Link>
              {" "}查看；清空影视与采集派生数据请使用下方「数据重置」。
            </>
          }
        />
      )}


      <Card
        className={styles.card}
        title={
          <Space size={8} align="center">
            <CloudSyncOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>数据备份与恢复</span>
          </Space>
        }
      >

        <Flex vertical gap={16}>
          <div className={styles.sectionHead}>
            <div className={styles.sectionText}>
              <Typography.Text strong>导出配置</Typography.Text>
              <Typography.Text type="secondary">
                下载 JSON 备份，便于环境迁移或操作前留档。
              </Typography.Text>
            </div>
            <Button
              type="primary"
              icon={<DownloadOutlined />}
              loading={exporting}
              disabled={!canWrite}
              onClick={() => void handleExport()}
            >
              导出配置
            </Button>
          </div>

          <div className={styles.sectionHead}>
            <div className={styles.sectionText}>
              <Typography.Text strong>导入配置</Typography.Text>
              <Typography.Text type="secondary">
                选择备份文件后按模块覆盖；需管理密码。导入采集站/任务时会先停止进行中的采集。
              </Typography.Text>
            </div>
            <Space wrap>
              <Upload
                accept=".json,application/json"
                showUploadList={false}
                beforeUpload={beforeUpload}
                disabled={!canWrite}
              >
                <Button icon={<UploadOutlined />} disabled={!canWrite}>
                  选择备份文件
                </Button>
              </Upload>
              <Button type="primary" disabled={!canWrite || !backup} onClick={openImport}>
                导入配置
              </Button>
            </Space>
          </div>
          {backup ? (
            <div className={styles.importPreview}>
              已选择：{fileName || "备份文件"}
              {backupSummary ? ` · ${backupSummary}` : ""}
            </div>
          ) : null}
        </Flex>
      </Card>

      <ResetSiteDataCard />

      <Modal
        title="导入配置备份"
        open={importOpen}
        onCancel={closeImport}
        onOk={() => void confirmImport()}
        okText="确认导入"
        confirmLoading={importing}
        okButtonProps={{ danger: true }}
        destroyOnHidden
        width={520}
      >
        <Flex vertical gap={14}>
          <Alert
            type="warning"
            showIcon
            title="导入将覆盖所选模块的现有配置，且不可自动回滚"
          />
          <div>
            <Typography.Text type="secondary">导入模块</Typography.Text>
            <div className={styles.moduleGrid}>
              {MODULE_OPTIONS.map((item) => (
                <Checkbox
                  key={item.key}
                  checked={modules[item.key]}
                  onChange={(e) =>
                    setModules((prev) => ({ ...prev, [item.key]: e.target.checked }))
                  }
                >
                  {item.label}
                </Checkbox>
              ))}
            </div>
          </div>
          {backupSummary ? (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {backupSummary}
            </Typography.Text>
          ) : null}
          <Input.Password
            placeholder="请输入管理密码"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </Flex>
      </Modal>
    </div>
  );
}
