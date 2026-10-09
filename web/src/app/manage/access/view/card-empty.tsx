"use client";

import type { CSSProperties } from "react";
import { Empty } from "antd";
import styles from "./index.module.less";

export const fillCardStyles = {
  root: {
    display: "flex",
    flexDirection: "column",
    height: "auto",
  } satisfies CSSProperties,
  body: {
    flex: 1,
    display: "flex",
    flexDirection: "column",
    minHeight: 0,
  } satisfies CSSProperties,
};

export default function CardEmpty({ description }: { description: string }) {
  return (
    <div className={styles.emptyState}>
      <Empty description={description} styles={{ root: { margin: 0 } }} />
    </div>
  );
}
