"use client";

import React from "react";
import { Select, Tag } from "antd";
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

/** 内容管理统一采集源选择器，内置「采集源」前缀并与主站/非主站标签联动。 */
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
        placeholder="选择采集源"
        className={styles.sourceSelect}
        // 选中值用 1 倍行高才能和前缀齐，但行高一变选择器会被内容撑矮，高度要钉回 token
        styles={{
          root: { height: "var(--ant-select-height)" },
          prefix: { display: "flex", alignItems: "center" },
          content: { lineHeight: 1 },
        }}
        prefix={
          <span className={styles.sourcePrefix}>
            <DatabaseOutlined />
            采集源
          </span>
        }
        value={sourceId || undefined}
        disabled={disabled}
        onChange={(value) => onChange(String(value))}
        options={sources.map((source) => ({
          label: source.state === false ? `${source.name} (已停用)` : source.name,
          value: source.id,
        }))}
        showSearch
        optionFilterProp="label"
        popupMatchSelectWidth={false}
      />
      {sourceId && primaryId ? (
        <Tag color={isPrimary ? "processing" : "default"} style={{ margin: 0 }}>
          {isPrimary ? "主站" : "非主站"}
        </Tag>
      ) : null}
    </div>
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
  children,
}: ContentSectionCardProps) {
  return (
    <div className={styles.sectionCard}>
      <div className={styles.sectionCardHeader}>
        <ContentSectionHeader
          icon={icon}
          title={title}
          count={count}
          extra={extra}
          actions={actions}
        />
      </div>
      <div className={bodyPadding ? styles.sectionCardBody : undefined}>{children}</div>
      {footer ? <div className={styles.sectionCardFooter}>{footer}</div> : null}
    </div>
  );
}

export { styles as contentShellStyles };
