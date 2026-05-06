"use client";

import React from "react";
import { useRouter } from "next/navigation";
import { Button, Empty, Row, Col } from "antd";
import { PlaySquareOutlined } from "@ant-design/icons";
import AppLoading from "@/components/public/Loading";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import styles from "./index.module.less";

interface FilmItem {
  id: string;
  mid?: string; // Some internal APIs use mid
  name: string;
  picture: string;
  year: string;
  cName: string;
  area: string;
  language?: string;
  classTag?: string;
  remarks: string;
  blurb?: string;
}

function normalizeMetaValue(value?: string | number | null) {
  const text = String(value ?? "").trim();
  if (!text || text === "0") {
    return "";
  }
  return text;
}

function getPrimaryPlotTag(classTag?: string) {
  return normalizeMetaValue(classTag)
    .split(/[,，/|、\s]+/)
    .map((tag) => tag.trim())
    .find(Boolean) || "";
}

function buildFilmMetaTags(item: FilmItem) {
  return [
    normalizeMetaValue(item.year?.slice(0, 4)),
    normalizeMetaValue(item.area?.split(",")[0]),
    normalizeMetaValue(item.language),
    getPrimaryPlotTag(item.classTag),
  ].filter(Boolean);
}

interface FilmListProps {
  list: FilmItem[];
  col?: number;
  className?: string;
  loading?: boolean;
  onOpenPlayPage?: (id: string, href: string) => void;
}
// Internal Component for individual film card
function FilmCard({
  item,
  colProps,
  colClassName,
  handleOpenPlayPage,
}: {
  item: FilmItem;
  colProps: any;
  colClassName: string;
  handleOpenPlayPage: (id: string) => void;
}) {
  const [imgLoaded, setImgLoaded] = React.useState(false);
  const [imgError, setImgError] = React.useState(false);
  const imgRef = React.useRef<HTMLImageElement | null>(null);
  const id = item.mid || item.id;
  const metaTags = buildFilmMetaTags(item);

  React.useEffect(() => {
    setImgLoaded(false);
    setImgError(false);
    const frame = window.requestAnimationFrame(() => {
      const img = imgRef.current;
      if (!img || img.getAttribute("src") !== item.picture) {
        return;
      }
      if (img.complete && img.naturalWidth > 0) {
        setImgLoaded(true);
      }
      if (img.complete && img.naturalWidth === 0) {
        setImgError(true);
      }
    });
    return () => window.cancelAnimationFrame(frame);
  }, [item.picture]);

  if (id === "-99") return null;

  return (
    <Col key={id} {...colProps} className={colClassName}>
      <div className={styles.item} onClick={() => handleOpenPlayPage(id)}>
        <div className={`${styles.posterWrapper} ${!imgLoaded && !imgError ? styles.loadingBg : ""}`}>
          {!imgError && item.picture && (
            /* 影片卡片图片全部来自后端动态地址，这里维持原生 img 与懒加载策略 */
            /* eslint-disable-next-line @next/next/no-img-element */
            <img
              ref={imgRef}
              src={item.picture}
              className={`${styles.poster} ${imgLoaded ? styles.posterLoaded : ""}`}
              alt={item.name}
              onLoad={() => setImgLoaded(true)}
              onError={() => {
                setImgError(true);
                setImgLoaded(false);
              }}
              loading="lazy"
            />
          )}

          {/* Top Right Badge */}
          <span className={styles.remark}>{item.remarks}</span>

          {/* Bottom Tags (Always visible) */}
          <div className={styles.tagGroup}>
            {metaTags.map((tag, index) => (
              <span key={`${tag}-${index}`} className={styles.tag}>{tag}</span>
            ))}
          </div>

          {/* Hover Overlay - Premium Design */}
          <div className={styles.overlay}>
            <div className={styles.overlayContent}>
              <h3 className={styles.overlayTitle}>{item.name}</h3>
              <div className={styles.overlayMeta}>
                {metaTags.map((tag, index) => (
                  <React.Fragment key={`${tag}-${index}`}>
                    {index > 0 && <span className={styles.dot}>•</span>}
                    <span>{tag}</span>
                  </React.Fragment>
                ))}
              </div>
              <p className={styles.overlayBlurb}>
                {item.blurb || "暂无简介，点击查看更多精彩内容..."}
              </p>
              <Button
                type="primary"
                block
                icon={<PlaySquareOutlined />}
                className={styles.playBtn}
              >
                立即播放
              </Button>
            </div>
          </div>
        </div>

        <div className={styles.infoLine}>
          <span className={styles.name}>{item.name?.split("[")[0]}</span>
          <span className={styles.subText}>{item.remarks}</span>
        </div>
      </div>
    </Col>
  );
}

export default function FilmList({
  list,
  col,
  className,
  loading = false,
  onOpenPlayPage,
}: FilmListProps) {
  const router = useRouter();

  const { props: colProps, className: colClassName } = React.useMemo(() => {
    // Default to 6 columns if not specified
    const targetCol = col || 7;

    // Standard AntD span calculation (for 24-grid system)
    // 1 -> 24, 2 -> 12, 3 -> 8, 4 -> 6, 6 -> 4, 8 -> 3, 12 -> 2
    const span = 24 % targetCol === 0 ? 24 / targetCol : 4;

    // Use specific CSS overrides for non-perfect divisors or to lock layout at 1440px
    const classNameMap: Record<number, string> = {
      5: styles["grid-col-5"],
      6: styles["grid-col-6"],
      7: styles["grid-col-7"],
      8: styles["grid-col-8"],
    };

    return {
      props: {
        xs: 8, // 3 columns on mobile
        sm: 6, // 4 columns on small tablets
        md: 6, // 4 columns on tablets
        // At lg (992px), SideList appears on home, taking 200px.
        // If col=6, jumping to 6 items here makes them too small (688px/6 = ~114px).
        // Stay at 4 items (696px/4 = ~174px) until xl (1200px).
        lg: targetCol >= 6 ? 6 : span,
        xl: span,
        xxl: span,
      },
      className: classNameMap[targetCol] || "",
    };
  }, [col]);

  // Loading State - global loading
  if (loading) {
    return (
      <div className={className || ""} style={{ minHeight: 280 }}>
        <AppLoading text="正在加载影片列表..." padding="88px 0" size="default" />
      </div>
    );
  }

  if (!list || list.length === 0) {
    return (
      <div style={{ padding: "40px 0", width: "100%" }}>
        <Empty description="暂无相关数据" />
      </div>
    );
  }

  const handleOpenPlayPage = (id: string) => {
    const href = resolvePlayEntryPath(id);
    if (onOpenPlayPage) {
      onOpenPlayPage(id, href);
      return;
    }
    router.push(href);
  };

  return (
    <div className={className || ""}>
      <Row gutter={[12, 12]}>
        {list.map((item) => (
          <FilmCard
            key={item.mid || item.id}
            item={item}
            colProps={colProps}
            colClassName={colClassName}
            handleOpenPlayPage={handleOpenPlayPage}
          />
        ))}
      </Row>
    </div>
  );
}
