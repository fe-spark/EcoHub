"use client";

import React from "react";
import { Button, Col, Empty, Row } from "antd";
import { PlaySquareOutlined, StarFilled, VideoCameraOutlined } from "@ant-design/icons";
import AppLoading from "@/components/public/Loading";
import HighlightMatchedText from "@/components/public/HighlightMatchedText";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import { isLocalFilmID, resolvePlayEntryPath } from "@/lib/playNavigation";
import styles from "./index.module.less";

interface FilmItem {
  id: string | number;
  mid?: string;
  name: string;
  picture: string;
  year?: string | number;
  cName?: string;
  area?: string;
  language?: string;
  classTag?: string;
  remarks?: string;
  blurb?: string;
  score?: number | string;
  actor?: string;
  director?: string;
  hits?: number | string;
  sourceId?: string;
  sourceMid?: string | number;
}

function normalizeMetaValue(value?: string | number | null) {
  const text = String(value ?? "").trim();
  if (!text || text === "0") {
    return "";
  }
  return text;
}

function getPrimaryPlotTag(classTag?: string | number | null) {
  return normalizeMetaValue(classTag)
    .split(/[,，/|、\s]+/)
    .map((tag) => tag.trim())
    .find(Boolean) || "";
}

function extractFilmMetadata(item: FilmItem) {
  const cNameStr = normalizeMetaValue(
    item.cName ||
      (item as any).c_name ||
      (item as any).typeName ||
      (item as any).type_name ||
      (item as any).category,
  );
  const yearStr = normalizeMetaValue(item.year);
  const formattedYear = yearStr ? yearStr.slice(0, 4) : "";
  const plotTag = getPrimaryPlotTag(
    item.classTag ||
      (item as any).class_tag ||
      (item as any).plot ||
      (item as any).tags,
  );
  const rawArea = normalizeMetaValue(item.area);
  const area = rawArea ? rawArea.split(/[,，/]/)[0].trim() : "";
  const remarks = normalizeMetaValue(item.remarks);
  const scoreNum = Number(item.score);
  const scoreStr = scoreNum > 0 ? scoreNum.toFixed(1) : "";

  // 提取主演或简评看点
  const rawActor = normalizeMetaValue(item.actor);
  const actor = rawActor ? rawActor.split(/[,，/]/).slice(0, 2).join(" · ") : "";
  const blurb = normalizeMetaValue(item.blurb);
  const castOrBlurb = actor ? `主演: ${actor}` : (blurb ? blurb.slice(0, 24) : "");

  // 左上角综合分类/题材标签
  const categoryLabel = cNameStr || plotTag || "";

  // 下方副标题行：优先主演/看点，若无则展示极简年份·题材·地区
  const fallbackMeta = [formattedYear, plotTag, area].filter(Boolean).join(" · ");
  const subText = castOrBlurb || fallbackMeta || remarks;

  // 拼接完整 tooltip 提示
  const allTags = [cNameStr, formattedYear, plotTag, area, scoreStr ? `${scoreStr}分` : ""].filter(Boolean);
  const metaText = allTags.join(" · ") || remarks;

  return {
    cNameStr,
    categoryLabel,
    formattedYear,
    plotTag,
    area,
    remarks,
    scoreStr,
    castOrBlurb,
    subText,
    metaText,
  };
}

interface FilmListProps {
  list: FilmItem[];
  col?: number;
  className?: string;
  loading?: boolean;
  highlightQuery?: string;
  onOpenPlayPage?: (id: string, href: string) => void;
}

// Internal Component for individual film card
function FilmCard({
  item,
  colProps,
  colClassName,
  highlightQuery,
  handleOpenPlayPage,
}: {
  item: FilmItem;
  colProps: any;
  colClassName: string;
  highlightQuery?: string;
  handleOpenPlayPage: (id: string, item?: FilmItem) => void;
}) {
  const [imgLoaded, setImgLoaded] = React.useState(false);
  const [imgError, setImgError] = React.useState(false);
  const imgRef = React.useRef<HTMLImageElement | null>(null);
  const id = Number(item.id) > 0 ? String(item.id) : "";

  const {
    categoryLabel,
    formattedYear,
    plotTag,
    area,
    remarks,
    scoreStr,
    subText,
    metaText,
  } = React.useMemo(() => extractFilmMetadata(item), [item]);

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

  if (String(item.id) === "-99") return null;

  return (
    <Col key={id} {...colProps} className={colClassName}>
      <div className={styles.item} onClick={() => handleOpenPlayPage(id, item)}>
        {/* 海报主视觉容器 */}
        <div className={`${styles.posterWrapper} ${!imgLoaded && !imgError ? styles.loadingBg : ""}`}>
          {!imgError && item.picture && (
            /* eslint-disable-next-line @next/next/no-img-element */
            <img
              ref={imgRef}
              src={item.picture}
              className={`${styles.poster} ${imgLoaded ? styles.posterLoaded : ""}`}
              alt={item.name}
              referrerPolicy="no-referrer"
              onLoad={() => setImgLoaded(true)}
              onError={() => {
                setImgError(true);
                setImgLoaded(false);
              }}
              loading="lazy"
            />
          )}

          {(imgError || !item.picture) && (
            <div className={styles.placeholderFallback}>
              <VideoCameraOutlined className={styles.placeholderIcon} />
              <span className={styles.placeholderTitle}>{item.name?.split("[")[0]}</span>
            </div>
          )}

          {/* 顶部标签行：Flex 左右两端对齐，彻底根除多徽标重叠 */}
          <div className={styles.posterTopBadges}>
            {categoryLabel ? (
              <span className={styles.categoryBadge} title={categoryLabel}>
                {categoryLabel}
              </span>
            ) : null}
            {scoreStr ? (
              <span className={styles.scoreBadge} title={`评分: ${scoreStr}`}>
                <StarFilled className={styles.scoreIcon} />
                <span>{scoreStr}</span>
              </span>
            ) : null}
          </div>

          {/* 右下角：流媒体沉浸式更新状态（底部自然微渐变，去除突兀色块标签） */}
          {remarks ? (
            <div className={styles.posterBottom}>
              <span className={styles.bottomRemark} title={remarks}>
                {remarks}
              </span>
            </div>
          ) : null}

          {/* Hover 浮层：沉浸式磨砂浮层 + 简介 + 底部弹出【立即播放】大按钮 */}
          <div className={styles.overlay}>
            <div className={styles.overlayContent}>
              <h3 className={styles.overlayTitle}>
                <HighlightMatchedText text={item.name?.split("[")[0]} query={highlightQuery} />
              </h3>
              <div className={styles.overlayMeta}>
                {[formattedYear, plotTag, area].filter(Boolean).map((t, idx) => (
                  <React.Fragment key={`${t}-${idx}`}>
                    {idx > 0 && <span className={styles.metaDot}>•</span>}
                    <span>{t}</span>
                  </React.Fragment>
                ))}
                {scoreStr ? (
                  <>
                    <span className={styles.metaDot}>•</span>
                    <span className={styles.overlayScore}>★ {scoreStr}</span>
                  </>
                ) : null}
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

        {/* 下方元信息区：仅保留片名 + 看点/副标题 */}
        <div className={styles.infoLine}>
          <span className={styles.name} title={item.name?.split("[")[0]}>
            <HighlightMatchedText text={item.name?.split("[")[0]} query={highlightQuery} />
          </span>

          {subText && (
            <span className={styles.subText} title={metaText}>
              {subText}
            </span>
          )}
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
  highlightQuery,
  onOpenPlayPage,
}: FilmListProps) {
  const { navigate } = useContentNavigate();

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

  const handleOpenPlayPage = (id: string, item?: FilmItem) => {
    const isLocal = isLocalFilmID(id);
    const href = resolvePlayEntryPath(id, {
      sourceId: isLocal ? undefined : item?.sourceId,
      sourceMid: item?.sourceMid,
    });
    if (onOpenPlayPage) {
      onOpenPlayPage(id, href);
      return;
    }
    navigate(href, "进入播放页...");
  };

  return (
    <div className={className || ""}>
      <Row gutter={[12, 12]}>
        {list.map((item) => (
          <FilmCard
            key={`${item.sourceId || ""}:${item.id || 0}:${item.sourceMid || item.mid || ""}`}
            item={item}
            colProps={colProps}
            colClassName={colClassName}
            highlightQuery={highlightQuery}
            handleOpenPlayPage={handleOpenPlayPage}
          />
        ))}
      </Row>
    </div>
  );
}
