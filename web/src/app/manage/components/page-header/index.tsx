import React from "react";
import { Space, Typography } from "antd";
import styles from "./index.module.less";

interface ManagePageHeaderProps {
  title: string;
  description?: React.ReactNode;
  actions?: React.ReactNode;
}

export default function ManagePageHeader(props: ManagePageHeaderProps) {
  const { title, description, actions } = props;
  const actionNodes = React.Children.toArray(actions);

  return (
    <div className={styles.header}>
      <div className={styles.content}>
        <Typography.Title level={3} className={styles.title}>
          {title}
        </Typography.Title>
        {description ? <Typography.Text type="secondary">{description}</Typography.Text> : null}
      </div>
      {actionNodes.length > 0 ? <Space wrap className={styles.actions}>{actionNodes}</Space> : null}
    </div>
  );
}
