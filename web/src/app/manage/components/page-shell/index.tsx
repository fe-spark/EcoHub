import React from "react";
import styles from "./index.module.less";

interface ManagePageShellProps {
  leading?: React.ReactNode;
  eyebrow?: string;
  title: string;
  description?: string;
  actions?: React.ReactNode;
  extra?: React.ReactNode;
  children: React.ReactNode;
  panelClassName?: string;
  panelless?: boolean;
}

export default function ManagePageShell(props: ManagePageShellProps) {
  const {
    leading,
    eyebrow,
    title,
    description,
    actions,
    extra,
    children,
    panelClassName,
    panelless,
  } = props;

  return (
    <div className={styles.page}>
      <section className={styles.hero}>
        <div className={styles.heroMain}>
          {leading ? <div className={styles.leading}>{leading}</div> : null}
          {eyebrow ? <div className={styles.eyebrow}>{eyebrow}</div> : null}
          <h2 className={styles.title}>{title}</h2>
          {description ? (
            <p className={styles.description}>{description}</p>
          ) : null}
        </div>
        {actions ? <div className={styles.actions}>{actions}</div> : null}
      </section>

      {extra ? <div className={styles.extra}>{extra}</div> : null}

      {panelless ? (
        <div className={panelClassName}>{children}</div>
      ) : (
        <section className={`${styles.panel} ${panelClassName || ""}`.trim()}>
          {children}
        </section>
      )}
    </div>
  );
}
