import React from "react";
import styles from "./index.module.less";

interface ManagePageShellProps {
  eyebrow?: string;
  title: string;
  description?: string;
  actions?: React.ReactNode;
  extra?: React.ReactNode;
  children: React.ReactNode;
  panelClassName?: string;
}

export default function ManagePageShell(props: ManagePageShellProps) {
  const { eyebrow, title, description, actions, extra, children, panelClassName } = props;

  return (
    <div className={styles.page}>
      <section className={styles.hero}>
        <div className={styles.heroMain}>
          {eyebrow ? <div className={styles.eyebrow}>{eyebrow}</div> : null}
          <h2 className={styles.title}>{title}</h2>
          {description ? <p className={styles.description}>{description}</p> : null}
        </div>
        {actions ? <div className={styles.actions}>{actions}</div> : null}
      </section>

      {extra ? <div className={styles.extra}>{extra}</div> : null}

      <section className={`${styles.panel} ${panelClassName || ""}`.trim()}>{children}</section>
    </div>
  );
}
