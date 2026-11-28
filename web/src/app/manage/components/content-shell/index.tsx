"use client";

import React, { useEffect } from "react";
import { Alert, Button, Flex, Select, Space, Tag } from "antd";
import { DatabaseOutlined } from "@ant-design/icons";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

export interface ContentSourceOption {
  id: string;
  name: string;
  state?: boolean;
}

interface ContentPageShellProps {
  title: string;
  description?: React.ReactNode;
  headerActions?: React.ReactNode;
  dirty?: boolean;
  children: React.ReactNode;
}

/** 内容管理统一页面骨架：页头与主体区保持一致的 16px 纵向节奏。 */
export function ContentPageShell({
  title,
  description,
  headerActions,
  dirty = false,
  children,
}: ContentPageShellProps) {
  useEffect(() => {
    if (!dirty) return;
    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => window.removeEventListener("beforeunload", handleBeforeUnload);
  }, [dirty]);

  return (
    <div className={`${styles.pageShell} ${dirty ? styles.pageShellDirty : ""}`}>
      <ManagePageHeader
        className={styles.pageHeader}
        title={title}
        description={description}
        actions={headerActions}
      />
      {children}
    </div>
  );
}

interface ContentFilterBarProps {
  lead?: React.ReactNode;
  actions?: React.ReactNode;
}

/** 内容管理统一筛选工具条：紧凑单行排布，不加多余外框卡片。 */
export function ContentFilterBar({ lead, actions }: ContentFilterBarProps) {
  return (
    <div className={styles.filterBar}>
      {lead ? <div className={styles.filterLead}>{lead}</div> : null}
      {actions ? <div className={styles.filterActions}>{actions}</div> : null}
    </div>
  );
}

interface ContentSourceSelectProps {
  sources: ContentSourceOption[];
  sourceId: string;
  primaryId?: string;
  disabled?: boolean;
  onChange: (sourceId: string) => void;
}

/** 内容管理统一站点选择器，仅保留图标前缀与站名，并与主站/非主站标签联动。 */
export function ContentSourceSelect({
  sources,
  sourceId,
  primaryId,
  disabled = false,
  onChange,
}: ContentSourceSelectProps) {
  const isPrimary = Boolean(sourceId && primaryId && sourceId === primaryId);
  return (
    <div className={styles.sourceField}>
      <Select
        aria-label="选择站点"
        placeholder="选择站点"
        className={styles.sourceSelect}
        // 选中值用 1 倍行高才能和前缀齐，但行高一变选择器会被内容撑矮，高度要钉回 token
        styles={{
          root: { height: "var(--ant-select-height)" },
          prefix: { display: "flex", alignItems: "center" },
          content: { lineHeight: 1 },
        }}
        prefix={
          <span className={styles.sourcePrefix} aria-hidden="true">
            <DatabaseOutlined />
          </span>
        }
        value={sourceId || undefined}
        disabled={disabled}
        onChange={(value) => onChange(String(value))}
        options={sources.map((source) => ({
          label: source.name,
          value: source.id,
          isPrimary: Boolean(primaryId && source.id === primaryId),
          disabledState: source.state === false,
        }))}
        optionRender={(option) => {
          const data = option.data as {
            label: string;
            isPrimary: boolean;
            disabledState: boolean;
          };
          return (
            <Flex align="center" justify="space-between" gap={8}>
              <span>{data.label}</span>
              {data.isPrimary || data.disabledState ? (
                <Space size={4}>
                  {data.isPrimary ? (
                    <Tag color="gold" variant="filled" style={{ margin: 0 }}>
                      主站
                    </Tag>
                  ) : null}
                  {data.disabledState ? (
                    <Tag variant="filled" style={{ margin: 0 }}>
                      已停用
                    </Tag>
                  ) : null}
                </Space>
              ) : null}
            </Flex>
          );
        }}
        showSearch
        optionFilterProp="label"
        popupMatchSelectWidth={false}
      />
      {sourceId && primaryId ? (
        <Tag color={isPrimary ? "processing" : "warning"} style={{ margin: 0 }}>
          {isPrimary ? "当前主站" : "非主站"}
        </Tag>
      ) : null}
    </div>
  );
}

interface ContentNonPrimaryAlertProps {
  sources: ContentSourceOption[];
  sourceId: string;
  primaryId?: string;
  primaryName?: string;
  scopeLabel: string;
  disabled?: boolean;
  onSwitchToPrimary: (primaryId: string) => void;
}

/** 预配置非主站提示条：明确当前仅修改指定备用站配置，并提供一键切回当前主站入口。 */
export function ContentNonPrimaryAlert({
  sources,
  sourceId,
  primaryId,
  primaryName,
  scopeLabel,
  disabled = false,
  onSwitchToPrimary,
}: ContentNonPrimaryAlertProps) {
  if (!sourceId || !primaryId || sourceId === primaryId) {
    return null;
  }
  const currentName = sources.find((item) => item.id === sourceId)?.name || sourceId;
  const resolvedPrimaryName =
    primaryName || sources.find((item) => item.id === primaryId)?.name || "";

  return (
    <Alert
      type="warning"
      showIcon
      title={`正在预配置非主站「${currentName}」的${scopeLabel}，保存仅作用于该站点，不会变更全局主站或线上前台展示`}
      action={
        <Button size="small" disabled={disabled} onClick={() => onSwitchToPrimary(primaryId)}>
          {resolvedPrimaryName ? `切回主站「${resolvedPrimaryName}」` : "切回主站"}
        </Button>
      }
    />
  );
}

interface ContentSectionHeaderProps {
  icon?: React.ReactNode;
  title: string;
  count?: number;
  extra?: React.ReactNode;
  actions?: React.ReactNode;
}

/** 内容管理统一分区标题栏，用于 Table title 或卡片头部。 */
export function ContentSectionHeader({
  icon,
  title,
  count,
  extra,
  actions,
}: ContentSectionHeaderProps) {
  return (
    <div className={styles.sectionHeader}>
      <div className={styles.sectionTitleGroup}>
        {icon ? <span className={styles.sectionIcon}>{icon}</span> : null}
        <span className={styles.sectionTitle}>{title}</span>
        {typeof count === "number" ? <span className={styles.countBadge}>{count}</span> : null}
        {extra}
      </div>
      {actions ? <div className={styles.sectionActions}>{actions}</div> : null}
    </div>
  );
}

interface ContentSectionCardProps {
  icon?: React.ReactNode;
  title: string;
  count?: number;
  extra?: React.ReactNode;
  actions?: React.ReactNode;
  footer?: React.ReactNode;
  bodyPadding?: boolean;
  className?: string;
  bodyClassName?: string;
  children: React.ReactNode;
}

/** 内容管理统一主体卡片：头部标题与操作、主体内容（表格或网格）、底部分页保持一致。 */
export function ContentSectionCard({
  icon,
  title,
  count,
  extra,
  actions,
  footer,
  bodyPadding = false,
  className,
  bodyClassName,
  children,
}: ContentSectionCardProps) {
  const cardClassName = [styles.sectionCard, className].filter(Boolean).join(" ");
  const resolvedBodyClass =
    [bodyPadding ? styles.sectionCardBody : "", bodyClassName].filter(Boolean).join(" ") ||
    undefined;

  return (
    <div className={cardClassName}>
      <div className={styles.sectionCardHeader}>
        <ContentSectionHeader
          icon={icon}
          title={title}
          count={count}
          extra={extra}
          actions={actions}
        />
      </div>
      <div className={resolvedBodyClass}>{children}</div>
      {footer ? <div className={styles.sectionCardFooter}>{footer}</div> : null}
    </div>
  );
}

export { styles as contentShellStyles };
