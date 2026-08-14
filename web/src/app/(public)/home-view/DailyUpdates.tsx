"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { SyncOutlined, VideoCameraOutlined } from "@ant-design/icons";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import { playDailyIonSwap, preloadPictures } from "./dailyIon";
import { bindPixelHover } from "./pixelHover";
import styles from "./DailyUpdates.module.less";

const REFRESH_MS = 15 * 1000;
const PAGE_SIZE = 6;

interface DailyFilm {
  id: string;
  mid?: string;
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

function filmId(item: DailyFilm) {
  return String(item.id ?? item.mid ?? "").trim();
}

function filmTitle(name?: string) {
  return (name ?? "").split("[")[0].trim();
}

function normalizeMeta(value?: string | number | null) {
  const text = String(value ?? "").trim();
  if (!text || text === "0") {
    return "";
  }
  return text;
}

function filmTags(item: DailyFilm) {
  const year = normalizeMeta(item.year?.slice(0, 4));
  const category = normalizeMeta(item.cName);
  const area = normalizeMeta(item.area?.split(",")[0]);
  const tags = [year, category];
  if (area && !category.includes(area)) {
    tags.push(area);
  }
  return tags.filter(Boolean);
}

function Poster({
  src,
  alt,
  className,
}: {
  src?: string;
  alt: string;
  className?: string;
}) {
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  const imgRef = useRef<HTMLImageElement | null>(null);

  useEffect(() => {
    setLoaded(false);
    setFailed(false);
    const frame = window.requestAnimationFrame(() => {
      const img = imgRef.current;
      if (!img || img.getAttribute("src") !== (src || "")) {
        return;
      }
      if (img.complete && img.naturalWidth > 0) {
        setLoaded(true);
      }
      if (img.complete && img.naturalWidth === 0) {
        setFailed(true);
      }
    });
    return () => window.cancelAnimationFrame(frame);
  }, [src]);

  return (
    <span className={`${styles.posterSlot} ${!loaded && !failed ? styles.loadingBg : ""}`}>
      {!failed && src ? (
        /* 封面来自采集源动态地址，保持原生 img */
        /* eslint-disable-next-line @next/next/no-img-element */
        <img
          ref={imgRef}
          src={src}
          alt={alt}
          className={`${className || ""} ${loaded ? styles.posterLoaded : ""}`}
          onLoad={() => setLoaded(true)}
          onError={() => {
            setFailed(true);
            setLoaded(false);
          }}
        />
      ) : null}
      {(failed || !src) && (
        <span className={styles.posterFallback} aria-hidden>
          <VideoCameraOutlined />
          <span>{alt || "暂无封面"}</span>
        </span>
      )}
    </span>
  );
}

export default function DailyUpdates() {
  const { navigate } = useContentNavigate();
  const [list, setList] = useState<DailyFilm[] | null>(null);
  const [tick, setTick] = useState(0);
  const [shuffling, setShuffling] = useState(false);
  const listRef = useRef<DailyFilm[] | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const cancelledRef = useRef(false);
  const inFlightRef = useRef(false);
  const ionBusyRef = useRef(false);
  const hoveringRef = useRef(false);
  const stageRef = useRef<HTMLDivElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const loadRef = useRef<(manual?: boolean) => Promise<void>>(async () => {});
  const [glowCurr, setGlowCurr] = useState("");
  const [glowPrev, setGlowPrev] = useState("");
  const [hovering, setHovering] = useState(false);

  const clearTimer = () => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = undefined;
    }
  };

  const scheduleNext = () => {
    if (cancelledRef.current || hoveringRef.current || inFlightRef.current) {
      return;
    }
    clearTimer();
    setTick((n) => n + 1);
    timerRef.current = setTimeout(() => {
      void loadRef.current();
    }, REFRESH_MS);
  };

  const goPlay = useCallback(
    (item: DailyFilm) => {
      const id = filmId(item);
      if (!id) {
        return;
      }
      navigate(resolvePlayEntryPath(id), "进入播放页...");
    },
    [navigate],
  );

  loadRef.current = async (manual = false) => {
    if (inFlightRef.current) {
      return;
    }
    inFlightRef.current = true;
    if (manual) {
      setShuffling(true);
    }

    const exclude = (listRef.current ?? []).map(filmId).filter(Boolean).join(",");
    const params = new URLSearchParams({ limit: String(PAGE_SIZE) });
    if (exclude) {
      params.set("exclude", exclude);
    }
    const url = `/api/index/dailyUpdates?${params.toString()}`;

    try {
      const res = await fetch(url, { cache: "no-store" });
      if (!res.ok) {
        throw new Error(String(res.status));
      }
      const json = (await res.json()) as { code: number; data?: DailyFilm[] };
      if (cancelledRef.current) {
        return;
      }
      if (json.code !== 0) {
        throw new Error(String(json.code));
      }
      const next = Array.isArray(json.data) ? json.data : [];
      if (next.length === 0) {
        return;
      }

      const apply = () => {
        listRef.current = next;
        setList(next);
      };

      const stage = stageRef.current;
      const canvas = canvasRef.current;
      const hasCurrent = (listRef.current ?? []).length > 0;
      if (!hasCurrent || !stage || !canvas || ionBusyRef.current) {
        apply();
        return;
      }

      ionBusyRef.current = true;
      try {
        await preloadPictures(next.map((item) => item.picture).filter(Boolean));
        if (cancelledRef.current) {
          return;
        }
        const slots = [...stage.querySelectorAll<HTMLElement>("[data-ion-slot]")];
        await playDailyIonSwap({
          canvas,
          stage,
          slots,
          nextPictures: next.map((item) => item.picture),
          onHide: () => {
            stage.classList.add(styles.stageIon);
          },
          onSwap: apply,
          onReveal: () => {
            stage.classList.remove(styles.stageIon);
          },
        });
      } catch {
        apply();
      } finally {
        stage.classList.remove(styles.stageIon);
        ionBusyRef.current = false;
      }
    } catch {
      // 保留当前批次，稍后重试
    } finally {
      inFlightRef.current = false;
      if (!cancelledRef.current) {
        setShuffling(false);
        scheduleNext();
      }
    }
  };

  useEffect(() => {
    cancelledRef.current = false;
    void loadRef.current();
    return () => {
      cancelledRef.current = true;
      clearTimer();
    };
  }, []);

  useEffect(() => {
    if (!list) {
      return;
    }
    const stage = stageRef.current;
    if (!stage) {
      return;
    }
    const unbindHover = bindPixelHover(stage);
    const onOver = (event: PointerEvent) => {
      const slot = (event.target as HTMLElement | null)?.closest?.("[data-pixel-hover]");
      if (!(slot instanceof HTMLElement) || !stage.contains(slot)) {
        return;
      }
      if (hoveringRef.current) {
        return;
      }
      hoveringRef.current = true;
      setHovering(true);
      clearTimer();
    };
    const onOut = (event: PointerEvent) => {
      const slot = (event.target as HTMLElement | null)?.closest?.("[data-pixel-hover]");
      if (!(slot instanceof HTMLElement)) {
        return;
      }
      const next = event.relatedTarget;
      if (
        next instanceof Element &&
        stage.contains(next) &&
        next.closest("[data-pixel-hover]")
      ) {
        return;
      }
      if (!hoveringRef.current) {
        return;
      }
      hoveringRef.current = false;
      setHovering(false);
      scheduleNext();
    };
    stage.addEventListener("pointerover", onOver);
    stage.addEventListener("pointerout", onOut);
    return () => {
      unbindHover();
      stage.removeEventListener("pointerover", onOver);
      stage.removeEventListener("pointerout", onOut);
    };
  }, [list ? 1 : 0]);

  const leadPicture = list?.[0]?.picture || "";
  useEffect(() => {
    if (!leadPicture || leadPicture === glowCurr) {
      return;
    }
    setGlowPrev(glowCurr);
    setGlowCurr(leadPicture);
  }, [glowCurr, leadPicture]);

  useEffect(() => {
    if (!glowPrev) {
      return;
    }
    const timer = window.setTimeout(() => setGlowPrev(""), 800);
    return () => window.clearTimeout(timer);
  }, [glowPrev]);

  if (!list || list.length === 0) {
    return null;
  }

  const lead = list[0];
  const rail = list.slice(1);
  const leadName = filmTitle(lead.name);
  const tags = filmTags(lead);

  return (
    <section className={styles.daily} aria-label="每日更新">
      <div className={styles.panel}>
        {glowPrev ? (
          <div
            className={`${styles.bandGlow} ${styles.bandGlowOut}`}
            style={{ backgroundImage: `url(${glowPrev})` }}
            aria-hidden
          />
        ) : null}
        {glowCurr ? (
          <div
            key={glowCurr}
            className={`${styles.bandGlow} ${styles.bandGlowIn}`}
            style={{ backgroundImage: `url(${glowCurr})` }}
            aria-hidden
          />
        ) : null}
        <div className={styles.inner}>
        <header className={styles.head}>
          <div className={styles.brand}>
            <span className={styles.live} aria-hidden />
            <div className={styles.brandCopy}>
              <p className={styles.eyebrow}>Just in · 近 24 小时</p>
              <h2 className={styles.title}>每日更新</h2>
            </div>
          </div>
          <div className={styles.tools}>
            <button
              type="button"
              className={styles.shuffle}
              onClick={() => {
                void loadRef.current(true);
              }}
              disabled={shuffling}
              aria-label="换一批"
            >
              <SyncOutlined spin={shuffling} />
              <span className={styles.shuffleText}>换一批</span>
            </button>
          </div>
        </header>

        <div className={styles.stage} ref={stageRef}>
          <canvas ref={canvasRef} className={styles.ionCanvas} aria-hidden />
          <article className={styles.lead}>
            <button
              type="button"
              className={styles.leadPoster}
              data-ion-slot
              data-pixel-hover
              onClick={() => goPlay(lead)}
              aria-label={`播放 ${leadName}`}
            >
              <Poster src={lead.picture} alt={leadName} className={styles.posterImg} />
              {lead.remarks ? <span className={styles.leadRemark}>{lead.remarks}</span> : null}
              <span className={styles.cardShade} />
              <span className={styles.cardMeta}>
                <span className={styles.cardName}>{leadName}</span>
              </span>
            </button>

            <div className={styles.leadSide}>
              <div className={styles.leadCopy}>
                <h3 className={styles.leadTitle}>{leadName}</h3>
                {tags.length > 0 ? (
                  <div className={styles.tags}>
                    {tags.map((tag) => (
                      <span key={tag} className={styles.tag}>
                        {tag}
                      </span>
                    ))}
                  </div>
                ) : null}
              </div>
              <p className={styles.blurb}>
                {lead.blurb || "近 24 小时新入库内容，点击即可观看最新进度。"}
              </p>

              {rail.length > 0 ? (
                <div className={styles.rail} aria-label="本批其他更新">
                  {rail.map((item, index) => {
                    const name = filmTitle(item.name);
                    return (
                      <button
                        key={index}
                        type="button"
                        className={styles.card}
                        data-ion-slot
                        data-pixel-hover
                        onClick={() => goPlay(item)}
                        aria-label={`播放 ${name}`}
                      >
                        <Poster src={item.picture} alt={name} className={styles.cardImg} />
                        <span className={styles.cardShade} />
                        {item.remarks ? <span className={styles.cardTip}>{item.remarks}</span> : null}
                        <span className={styles.cardMeta}>
                          <span className={styles.cardName}>{name}</span>
                        </span>
                      </button>
                    );
                  })}
                </div>
              ) : null}
            </div>
          </article>
        </div>
        </div>

        {!hovering ? (
          <div className={styles.progress} aria-hidden>
            <i key={tick} style={{ animationDuration: `${REFRESH_MS}ms` }} />
          </div>
        ) : null}
      </div>
    </section>
  );
}
