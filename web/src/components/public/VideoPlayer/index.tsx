"use client";

import React, { useEffect, useRef, useState } from "react";
import Artplayer from "artplayer";
import artplayerPluginDanmuku from "artplayer-plugin-danmuku";
import Hls from "hls.js";
import { Button, Result } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useSiteConfig } from "@/components/common/SiteGuard";
import styles from "./index.module.less";

interface VideoPlayerProps {
  src: string;
  filmId?: string | number;
  filmTitle?: string;
  episode?: number;
  poster?: string;
  autoplay?: boolean;
  initialTime?: number;
  onEnded?: () => void;
  onTimeUpdate?: (currentTime: number, duration: number) => void;
  onError?: (error: unknown) => void;
  onPlayStart?: () => void;
}

/**
 * 极简播放器组件：
 * 采用 "src as key" 策略。
 * 核心原则：加载失败即展示 UI 报错与重试按钮，不进行静默处理。
 */
const VideoPlayer: React.FC<VideoPlayerProps> = ({
  src,
  filmId,
  filmTitle,
  episode = 1,
  poster,
  autoplay = true,
  initialTime = 0,
  onEnded,
  onTimeUpdate,
  onError,
  onPlayStart,
}) => {
  const { config: siteConfig } = useSiteConfig();
  const danmakuEnabled = siteConfig?.danmaku ? siteConfig.danmaku.enabled !== false : true;
  const danmakuMetaRef = useRef({ filmId, filmTitle, episode });
  danmakuMetaRef.current = { filmId, filmTitle, episode };
  const artRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<Artplayer | null>(null);
  const hlsRef = useRef<Hls | null>(null);
  const [hasError, setHasError] = useState(false);
  const [retryCount, setRetryCount] = useState(0);

  // 回调保护
  const callbacks = useRef({ onEnded, onTimeUpdate, onError, onPlayStart });
  useEffect(() => {
    callbacks.current = { onEnded, onTimeUpdate, onError, onPlayStart };
  }, [onEnded, onTimeUpdate, onError, onPlayStart]);

  // 核心初始化 Effect
  useEffect(() => {
    let cancelled = false;
    let player: Artplayer | null = null;
    let modeTimer: any = null;
    let seekEndTimer: any = null;
    let miniRafId = 0;
    let onScrollOrResize: (() => void) | null = null;
    let miniObserver: IntersectionObserver | null = null;

    if (!artRef.current) {
      return () => {
        cancelled = true;
      };
    }

    const isMobile = /iPhone|iPad|iPod|Android/i.test(navigator.userAgent) || window.innerWidth <= 768;
    Artplayer.PLAYBACK_RATE = [0.5, 0.75, 1, 1.25, 1.5, 2, 2.5, 3];
    const isM3U8 = src.includes(".m3u8") || src.includes("m3u8");
    const pendingLive: { text: string; color?: string; mode?: 0 | 1 | 2 }[] = [];
    let listReady = !danmakuEnabled;
    const art = new Artplayer({
      container: artRef.current,
      url: src,
      type: isM3U8 ? "m3u8" : undefined,
      poster: poster || "",
      autoplay,
      theme: "#fa8c16",
      volume: 0.7,
      pip: false,
      autoMini: false,
      setting: true,
      playbackRate: true,
      aspectRatio: true,
      fullscreen: true,
      fullscreenWeb: !isMobile,
      mutex: true,
      backdrop: false,
      playsInline: true,
      autoPlayback: initialTime <= 0,
      airplay: true,
      useSSR: typeof window === "undefined",
      plugins: danmakuEnabled
        ? [
            artplayerPluginDanmuku({
              danmuku: [],
              speed: 5,
              opacity: 1,
              fontSize: 25,
              color: "#ffffff",
              mode: 0,
              modes: [0, 1, 2],
              antiOverlap: true,
              synchronousPlayback: true,
              beforeEmit: async (danmu) => {
                const meta = danmakuMetaRef.current;
                const mid = Number(meta.filmId);
                if (meta.filmId == null || meta.filmId === "" || Number.isNaN(mid)) {
                  art.notice.show = "缺少影片信息，无法发送弹幕";
                  return false;
                }
                try {
                  const resp = await ApiPost("/film/danmaku", {
                    mid,
                    episode: meta.episode || 1,
                    time: danmu.time || 0,
                    text: danmu.text,
                    color: danmu.color || "#ffffff",
                    mode: danmu.mode || 0,
                  });
                  if (resp.code !== 0) {
                    art.notice.show = resp.msg || "发送弹幕失败";
                    return false;
                  }
                  if (!listReady) {
                    pendingLive.push({
                      text: danmu.text,
                      color: danmu.color,
                      mode: danmu.mode === 1 ? 1 : danmu.mode === 2 ? 2 : 0,
                    });
                  } else {
                    sourceComments.push({
                      text: danmu.text,
                      time: danmu.time || art.currentTime,
                      color: danmu.color || "#ffffff",
                      mode: danmu.mode === 1 ? 1 : danmu.mode === 2 ? 2 : 0,
                    });
                  }
                  return true;
                } catch (e: any) {
                  art.notice.show = "发送弹幕失败: " + (e?.message || e);
                  return false;
                }
              },
            }),
          ]
        : [],
      customType: {
        m3u8: function (video: HTMLMediaElement, url: string) {
          if (Hls.isSupported()) {
            if (hlsRef.current) hlsRef.current.destroy();
            const hls = new Hls({
              enableWorker: true,
              maxBufferSize: 60 * 1024 * 1024,
              maxBufferLength: 30,
              maxMaxBufferLength: 60,
              backBufferLength: 30,
              maxBufferHole: 0.5,
              highBufferWatchdogPeriod: 2,
              nudgeOffset: 0.1,
              nudgeMaxRetry: 5,
              fragLoadingMaxRetry: 5,
              levelLoadingMaxRetry: 5,
              manifestLoadingMaxRetry: 3,
              startLevel: -1,
              abrEwmaDefaultEstimate: 5000000,
              testBandwidth: true,
            });
            hlsRef.current = hls;
            hls.loadSource(url);
            hls.attachMedia(video);
            hls.on(Hls.Events.MANIFEST_PARSED, () => {
              if (initialTime > 0) {
                video.currentTime = initialTime;
              }
            });
            let mediaRecoverTried = false;
            let networkRecoverTried = false;
            hls.on(Hls.Events.ERROR, (_, data) => {
              if (!data.fatal) return;
              console.error("HLS Fatal Error:", data);
              switch (data.type) {
                case Hls.ErrorTypes.NETWORK_ERROR:
                  if (!networkRecoverTried) {
                    networkRecoverTried = true;
                    hls.startLoad();
                  } else {
                    setHasError(true);
                    callbacks.current.onError?.(data);
                  }
                  break;
                case Hls.ErrorTypes.MEDIA_ERROR:
                  if (!mediaRecoverTried) {
                    mediaRecoverTried = true;
                    hls.recoverMediaError();
                  } else {
                    setHasError(true);
                    callbacks.current.onError?.(data);
                  }
                  break;
                default:
                  setHasError(true);
                  callbacks.current.onError?.(data);
              }
            });
          } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
            video.src = url;
            const onLoaded = () => {
              if (initialTime > 0) {
                video.currentTime = initialTime;
              }
              video.removeEventListener("loadedmetadata", onLoaded);
            };
            video.addEventListener("loadedmetadata", onLoaded);
          }
          video.setAttribute("playsinline", "true");
          video.setAttribute("webkit-playsinline", "true");
          if (/MicroMessenger/i.test(navigator.userAgent)) {
            video.setAttribute("x5-video-player-type", "h5");
            video.setAttribute("x5-video-player-fullscreen", "true");
          }
        },
      },
    });
    player = art;
    type DanmakuItem = { text: string; time: number; color: string; mode: 0 | 1 | 2 };
    let sourceComments: DanmakuItem[] = [];
    let placeGen = 0;
    let placeChain: Promise<void> = Promise.resolve();
    const travelSeconds = () => {
      const base = art.fullscreen || art.fullscreenWeb ? 10 : 5;
      const rate = Number(art.playbackRate) || 1;
      return base / rate;
    };
    const placeDanmaku = (now: number) => {
      const gen = ++placeGen;
      placeChain = placeChain
        .then(async () => {
          if (cancelled || gen !== placeGen) return;
          const dm = (art.plugins as any)?.artplayerPluginDanmuku;
          if (!dm || typeof dm.load !== "function") return;
          const duration = travelSeconds();
          const view = sourceComments.flatMap((item) => {
            const start = item.time || 0;
            const elapsed = now - start;
            if (elapsed < 0) return [item];
            if (elapsed < duration) return [{ ...item, time: now, id: `seek:${start}` }];
            return [];
          });
          await dm.load();
          if (cancelled || gen !== placeGen) return;
          if (view.length > 0) await dm.load(view);
        })
        .catch(() => {});
      return placeChain;
    };
    art.on("artplayerPluginDanmuku:visible", (danmu: any) => {
      const id = String(danmu?.id || "");
      if (!id.startsWith("seek:") || !danmu.$ref) return;
      const start = Number(id.slice(5));
      const duration = travelSeconds();
      const elapsed = art.currentTime - start;
      if (!(elapsed > 0 && elapsed < duration)) return;
      danmu.$restTime = duration - elapsed;
      danmu.$lastStartTime = Date.now();
      if (danmu.mode !== 0) return;
      const width = (art.template?.$player?.clientWidth || 0) + danmu.$ref.clientWidth;
      const remain = duration - elapsed;
      danmu.$ref.style.transition = "none";
      danmu.$ref.style.transform = `translateX(${-width * (elapsed / duration)}px)`;
      requestAnimationFrame(() => {
        if (!danmu.$ref) return;
        danmu.$ref.style.transition = `transform ${remain}s linear 0s`;
        danmu.$ref.style.transform = `translateX(${-width}px)`;
      });
    });
    if (danmakuEnabled) {
      void (async () => {
        const meta = danmakuMetaRef.current;
        let comments: DanmakuItem[] = [];
        if (meta.filmId != null && meta.filmId !== "") {
          try {
            const resp = await ApiGet<{ text: string; time: number; color?: string; mode?: number }[]>(
              "/film/danmaku",
              { mid: meta.filmId, episode: meta.episode || 1, title: meta.filmTitle || "" },
            );
            if (!cancelled && resp.code === 0 && Array.isArray(resp.data)) {
              comments = resp.data.map((item) => ({
                text: item.text,
                time: item.time,
                color: item.color || "#ffffff",
                mode: (item.mode === 1 ? 1 : item.mode === 2 ? 2 : 0) as 0 | 1 | 2,
              }));
            }
          } catch (e) {
            console.error("加载弹幕失败", e);
          }
        }
        if (cancelled) return;
        sourceComments = comments.concat(
          pendingLive.map((item) => ({
            text: item.text,
            time: art.currentTime,
            color: item.color || "#ffffff",
            mode: item.mode || 0,
          })),
        );
        pendingLive.length = 0;
        listReady = true;
        await placeDanmaku(art.currentTime);
      })();
    }
    if (art.template?.$video) {
      const v = art.template.$video;
      v.setAttribute("playsinline", "true");
      v.setAttribute("webkit-playsinline", "true");
      v.setAttribute("x5-playsinline", "true");
      v.setAttribute("x5-video-player-type", "h5");
      v.setAttribute("x5-video-player-fullscreen", "false");
    }

    playerRef.current = art;

    if (!isMobile && artRef.current) {
      const updateMiniState = () => {
        if (!artRef.current || !art) return;
        if (art.fullscreen || art.fullscreenWeb) {
          if (art.mini) art.mini = false;
          return;
        }
        const rect = artRef.current.getBoundingClientRect();
        if (rect.bottom < 100) {
          if (art.playing && !art.mini) art.mini = true;
        } else if (rect.top > -50) {
          if (art.mini) art.mini = false;
        }
      };
      const scheduleMiniCheck = () => {
        if (miniRafId) return;
        miniRafId = requestAnimationFrame(() => {
          miniRafId = 0;
          updateMiniState();
        });
      };
      onScrollOrResize = scheduleMiniCheck;
      window.addEventListener("scroll", scheduleMiniCheck, { passive: true });
      window.addEventListener("resize", scheduleMiniCheck);
      if (typeof IntersectionObserver !== "undefined") {
        miniObserver = new IntersectionObserver(scheduleMiniCheck, {
          root: null,
          rootMargin: "100px 0px 100px 0px",
          threshold: [0, 0.01, 1],
        });
        miniObserver.observe(artRef.current);
      }
      scheduleMiniCheck();
    }

    art.on("ready", () => {
      if (initialTime > 0 && !isM3U8) {
        art.currentTime = initialTime;
      }
    });
    const updateDanmukuMode = () => {
      const dm = (art.plugins as any)?.artplayerPluginDanmuku;
      if (!dm || typeof dm.config !== "function") return;
      dm.config({ speed: art.fullscreen || art.fullscreenWeb ? 10 : 5 });
      void placeDanmaku(art.currentTime);
    };
    const scheduleDanmukuMode = () => {
      clearTimeout(modeTimer);
      modeTimer = setTimeout(updateDanmukuMode, 50);
    };
    art.on("fullscreen", scheduleDanmukuMode);
    art.on("fullscreenWeb", scheduleDanmukuMode);

    art.on("video:ended", () => callbacks.current.onEnded?.());
    art.on("video:timeupdate", () =>
      callbacks.current.onTimeUpdate?.(art.currentTime, art.duration),
    );

    art.on("error", (err) => {
      if (
        err?.name === "NotAllowedError" ||
        String(err?.message || "").includes("NotAllowedError") ||
        String(err || "").includes("NotAllowedError")
      ) {
        console.warn("Autoplay was prevented by browser policy:", err);
        return;
      }
      console.error("Artplayer Error:", err);
      setHasError(true);
      callbacks.current.onError?.(err);
    });

    let isSeeking = false;
    let wasPlayingBeforeSeek = false;
    let seekGen = 0;
    let seekedGen = 0;
     let placeFrame = 0;
     const syncDanmukuSeek = () => {
       if (!danmakuEnabled) return;
       cancelAnimationFrame(placeFrame);
       placeFrame = requestAnimationFrame(() => {
         void placeDanmaku(art.currentTime);
       });
     };
     art.on("seek", syncDanmukuSeek);
 
     art.on("video:seeking", () => {
       isSeeking = true;
       seekGen += 1;
       clearTimeout(seekEndTimer);
       syncDanmukuSeek();
       const v = art.template?.$video;
       if (v && !v.paused) {
         wasPlayingBeforeSeek = true;
         v.pause();
       }
       if (art.loading) art.loading.show = true;
     });
 
     const resumePlaybackAfterSeek = () => {
       if (!isSeeking) return;
       isSeeking = false;
       if (art.loading) art.loading.show = false;
       if (wasPlayingBeforeSeek) {
         wasPlayingBeforeSeek = false;
         art.play().catch(() => {});
       }
     };
 
     art.on("video:seeked", () => {
       seekedGen = seekGen;
       if (!isSeeking) return;
       cancelAnimationFrame(placeFrame);
       void placeDanmaku(art.currentTime).then(() => {
         requestAnimationFrame(() => {
           const v = art.template?.$video;
           if (v && v.readyState >= 3) {
             resumePlaybackAfterSeek();
             return;
           }
           clearTimeout(seekEndTimer);
           seekEndTimer = setTimeout(resumePlaybackAfterSeek, 2500);
         });
       });
     });
 
     art.on("video:canplay", () => {
       if (!isSeeking || seekedGen !== seekGen) return;
       clearTimeout(seekEndTimer);
       resumePlaybackAfterSeek();
     });
 
     art.on("video:playing", () => {
       setHasError(false);
       callbacks.current.onPlayStart?.();
     });
 
     return () => {
       cancelled = true;
       clearTimeout(modeTimer);
       clearTimeout(seekEndTimer);
       if (placeFrame) cancelAnimationFrame(placeFrame);
      if (miniRafId) cancelAnimationFrame(miniRafId);
      if (onScrollOrResize) {
        window.removeEventListener("scroll", onScrollOrResize);
        window.removeEventListener("resize", onScrollOrResize);
      }
      miniObserver?.disconnect();
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
      if (player) {
        if (player.mini) player.mini = false;
        player.destroy(true);
      }
      playerRef.current = null;
    };
  }, [retryCount, src, danmakuEnabled]);

  useEffect(() => {
    if (playerRef.current && poster) playerRef.current.poster = poster;
  }, [poster]);

  return (
    <div className={styles.playerWrapper}>
      <div
        ref={artRef}
        style={{
          width: "100%",
          height: "100%",
          display: hasError ? "none" : "block",
        }}
      />
      {hasError && (
        <div className={styles.errorOverlay}>
          <Result
            status="error"
            title="视频加载失败"
            subTitle="该视频源可能已失效或受到环境策略限制，请尝试切换播放源或重新加载。"
            extra={[
              <Button
                type="primary"
                key="retry"
                icon={<ReloadOutlined />}
                className={styles.retryBtn}
                onClick={() => {
                  setHasError(false);
                  setRetryCount((p) => p + 1);
                }}
              >
                立即重试
              </Button>,
              <Button
                key="back"
                ghost
                className={styles.backBtn}
                onClick={() => window.location.reload()}
              >
                刷新页面
              </Button>,
            ]}
          />
        </div>
      )}
    </div>
  );
};

export default VideoPlayer;
