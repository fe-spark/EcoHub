import React from "react";
import { Alert } from "antd";
import type { StorageAlert } from "./types";
import styles from "./index.module.less";

interface StorageAlertsProps {
  alerts: StorageAlert[];
}

export default function StorageAlerts({ alerts }: StorageAlertsProps) {
  if (!alerts.length) return null;

  return (
    <>
      {alerts.map((item) => (
        <Alert
          key={item.key}
          type="error"
          showIcon
          title={item.title}
          description={
            <div className={styles.alertBody}>
              <div>{item.detail}</div>
              {item.steps?.length ? (
                <ol className={styles.alertSteps}>
                  {item.steps.map((step) => (
                    <li key={step}>{step}</li>
                  ))}
                </ol>
              ) : null}
            </div>
          }
          className={styles.storageAlert}
        />
      ))}
    </>
  );
}
