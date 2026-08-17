"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";
import { SyncOutlined, VideoCameraOutlined } from "@ant-design/icons";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import { playDailyIonSwap } from "./dailyIon";
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

async function fetchDailyUpdates(exclude: string): Promise<DailyFilm[]> {
  const params = new URLSearchParams({ limit: String(PAGE_SIZE) });
  if (exclude) {
    params.set("exclude", exclude);
  }
  const res = await fetch(`/api/index/dailyUpdates?${params.toString()}`, { cache: "no-store" });
  if (!res.ok) {
    throw new Error(String(res.status));
  }
  const json = (await res.json()) as { code: number; data?: DailyFilm[] };
  if (json.code !== 0) {
    throw new Error(String(json.code));
  }
  return Array.isArray(json.data) ? json.data : [];
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
    const current = imgRef.current;
    if (current && current.getAttribute("src") === (src || "") && current.complete) {
      setLoaded(current.naturalWidth > 0);
      setFailed(current.naturalWidth === 0);
      return;
    }
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
  const [copyLead, setCopyLead] = useState<DailyFilm | null>(null);

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
    clearTimer();
    const hasCurrent = (listRef.current ?? []).length > 0;
    if (manual || hasCurrent) {
      setShuffling(true);
    }

    const exclude = (listRef.current ?? []).map(filmId).filter(Boolean).join(",");
    const stage = stageRef.current;
    const canvas = canvasRef.current;
    const canIon = Boolean(hasCurrent && stage && canvas && !ionBusyRef.current);

    const loadNext = async () => {
      const next = await fetchDailyUpdates(exclude);
      if (cancelledRef.current || next.length === 0) {
        return null;
      }
      return {
        apply: () => {
          listRef.current = next;
          flushSync(() => {
            setList(next);
          });
        },
      };
    };

    try {
      if (!canIon || !stage || !canvas) {
        const result = await loadNext();
        result?.apply();
        const nextLead = listRef.current?.[0];
        if (nextLead) {
          setCopyLead(nextLead);
        }
        return;
      }

      ionBusyRef.current = true;
      const slots = [...stage.querySelectorAll<HTMLElement>("[data-ion-slot]")];
      const pending = loadNext().catch(() => null);
      try {
        await playDailyIonSwap({
          canvas,
          stage,
          slots,
          pending,
          onHide: () => {
            stage.classList.add(styles.stageIon);
          },
          onReveal: () => {
            stage.classList.remove(styles.stageIon);
          },
          onLeadReady: () => {
            const nextLead = listRef.current?.[0];
            if (nextLead) {
              setCopyLead(nextLead);
            }
          },
        });
      } catch {
        const result = await pending;
        result?.apply();
        const nextLead = listRef.current?.[0];
        if (nextLead) {
          setCopyLead(nextLead);
        }
      } finally {
        stage.classList.remove(styles.stageIon);
        stage.querySelectorAll("[data-ion-fx]").forEach((el) => {
          el.removeAttribute("data-ion-fx");
        });
        const ionCtx = canvas.getContext("2d");
        ionCtx?.clearRect(0, 0, canvas.width, canvas.height);
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

  useEffect(() => {
    if (list?.[0] && !copyLead) {
      setCopyLead(list[0]);
    }
  }, [copyLead, list]);

  const glowPicture = (copyLead ?? list?.[0])?.picture || "";
  useEffect(() => {
    if (!glowPicture || glowPicture === glowCurr) {
      return;
    }
    setGlowPrev(glowCurr);
    setGlowCurr(glowPicture);
  }, [glowCurr, glowPicture]);

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
  const shown = copyLead ?? lead;
  const leadName = filmTitle(lead.name);
  const shownName = filmTitle(shown.name);
  const tags = filmTags(shown);

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
              <div className={styles.leadCopy} key={filmId(shown)}>
                <h3 className={styles.leadTitle}>{shownName}</h3>
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
              <p className={styles.blurb} key={`blurb-${filmId(shown)}`}>
                {shown.blurb || "近 24 小时新入库内容，点击即可观看最新进度。"}
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

        <div className={styles.progress} aria-hidden>
          {!hovering && !shuffling ? (
            <i key={tick} style={{ animationDuration: `${REFRESH_MS}ms` }} />
          ) : null}
        </div>
      </div>
    </section>
  );
}
