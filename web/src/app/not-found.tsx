import Link from "next/link";
import styles from "./not-found.module.css";

export default function NotFound() {
  return (
    <main className={styles.page}>
      <section className={styles.card}>
        <p className={styles.code}>404</p>
        <h1 className={styles.title}>页面不存在</h1>
        <p className={styles.desc}>你访问的链接可能已失效，或该内容已被移动。</p>
        <Link href="/" className={styles.backLink}>
          返回首页
        </Link>
      </section>
    </main>
  );
}
