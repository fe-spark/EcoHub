import styles from "./index.module.less";

type Series = { t: string; pv: number; err4: number; err5: number; providePv: number }[];

export default function Sparkline({ series }: { series: Series | undefined }) {
  const pts = series || [];
  if (pts.length === 0) {
    return <div className={styles.sparkEmpty}>暂无曲线</div>;
  }
  const max = Math.max(1, ...pts.map((p) => Math.max(p.pv || 0, p.providePv || 0)));
  const w = 960;
  const h = 120;
  const toPoints = (key: "pv" | "providePv") =>
    pts
      .map((p, i) => {
        const x = pts.length === 1 ? 0 : (i / (pts.length - 1)) * w;
        const y = h - 8 - ((p[key] || 0) / max) * (h - 16);
        return `${x},${y}`;
      })
      .join(" ");
  return (
    <svg className={styles.spark} viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none">
      <polyline fill="none" stroke="#69b1ff" strokeWidth="2" points={toPoints("providePv")} />
      <polyline fill="none" stroke="#fa8c16" strokeWidth="2.5" points={toPoints("pv")} />
    </svg>
  );
}
