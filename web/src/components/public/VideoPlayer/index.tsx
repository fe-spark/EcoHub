"use client";

import React, { useEffect, useRef, useState } from "react";
import Artplayer from "artplayer";
import artplayerPluginDanmuku from "artplayer-plugin-danmuku";
import Hls from "hls.js";
import { Button, Result } from "antd";
import { CloseCircleFilled, ReloadOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useSiteConfig } from "@/components/common/SiteGuard";
import {
  MOBILE_DEFAULT_FONT_SIZE,
  changedDanmakuSettings,
  loadDanmakuSettings,
  pickDanmakuSettings,
  saveDanmakuSettings,
} from "./danmakuSettings";
import {
  DANMAKU_LOADING_STATUS,
  danmakuLoadedStatus,
  danmakuRequestFailedStatus,
  type DanmakuMeta,
  type DanmakuStatus,
} from "./danmakuStatus";
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
  const danmakuEnabled = Boolean(siteConfig?.danmaku?.enabled);
  const danmakuMetaRef = useRef({ filmId, filmTitle, episode });
  useEffect(() => {
    danmakuMetaRef.current = { filmId, filmTitle, episode };
  }, [filmId, filmTitle, episode]);
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
    let player: Artplayer | null = null;
    let miniRafId = 0;
    let onScrollOrResize: (() => void) | null = null;
    let miniObserver: IntersectionObserver | null = null;

    if (!artRef.current) return;

    const isMobile = /iPhone|iPad|iPod|Android/i.test(navigator.userAgent) || window.innerWidth <= 768;
    Artplayer.PLAYBACK_RATE = [0.5, 0.75, 1, 1.25, 1.5, 2, 2.5, 3];
    const isM3U8 = src.includes(".m3u8") || src.includes("m3u8");
    type DanmakuMode = 0 | 1 | 2;
    const toMode = (m?: number): DanmakuMode => (m === 1 ? 1 : m === 2 ? 2 : 0);
    // 弹幕列表返回前用户已发出的弹幕：插件 load 完会清空队列，这里记下来随列表一起装回去
    const sentBeforeLoad: { text: string; time: number; color: string; mode: DanmakuMode }[] = [];
    let listLoaded = false;
    // 弹幕加载状态显示在输入框上（灰字 + 悬停说明 + 左侧状态点），不再用 notice 盖在画面上。
    // 插件控件可能晚于本函数挂上，记下最新状态，ready 时再补一次。
    let danmakuStatus: DanmakuStatus | null = null;
    const renderDanmakuStatus = () => {
      const $player = art?.template?.$player as HTMLElement | undefined;
      const $input = $player?.querySelector(".apd-input") as HTMLInputElement | null;
      if (!danmakuStatus || !$input) return;
      $input.placeholder = danmakuStatus.placeholder;
      $input.title = danmakuStatus.title;
      let $dot = $player?.querySelector(".ecohub-danmaku-status") as HTMLElement | null;
      if (!$dot) {
        $dot = document.createElement("span");
        $dot.className = "ecohub-danmaku-status";
        $input.before($dot);
      }
      $dot.dataset.tone = danmakuStatus.tone;
      $dot.title = danmakuStatus.title;
    };
    const showDanmakuStatus = (status: DanmakuStatus) => {
      danmakuStatus = status;
      renderDanmakuStatus();
    };
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
              margin: [10, "50%"],
              // 桌面沿用插件默认 25px；手机按播放器高度取字号，避免 25px 在约 190px 高的画面里占掉一大片
              fontSize: isMobile ? MOBILE_DEFAULT_FONT_SIZE : 25,
              ...loadDanmakuSettings(isMobile),
              synchronousPlayback: true,
              // 不按播放器宽度把弹幕栏挪到播放器外面（外层容器 overflow:hidden 会把它裁掉）
              width: 0,
              // 手机只保留开关和设置，不显示输入框
              emitter: !isMobile,
              // 插件创建时自己调用一次，只拉一次列表
              danmuku: async () => {
                // 插件在 new Artplayer() 里同步调用本函数，先让出一拍，等 art 赋值后再用
                await Promise.resolve();
                const meta = danmakuMetaRef.current;
                let list: { text: string; time: number; color: string; mode: DanmakuMode }[] = [];
                if (meta.filmId != null && meta.filmId !== "") {
                  showDanmakuStatus(DANMAKU_LOADING_STATUS);
                  let status: DanmakuStatus;
                  try {
                    const resp = await ApiGet<{ text: string; time: number; color?: string; mode?: number }[]>(
                      "/film/danmaku",
                      { mid: meta.filmId, episode: meta.episode || 1, title: meta.filmTitle || "" },
                    );
                    if (resp.code === 0 && Array.isArray(resp.data)) {
                      list = resp.data.map((item) => ({
                        text: item.text,
                        time: Number(item.time) || 0,
                        color: item.color || "#ffffff",
                        // 手机播放器只有三四行高，插件只在同类弹幕之间避让，顶部/底部固定弹幕会压在滚动弹幕上；
                        // 手机上统一按滚动弹幕显示，全部参与同一套避让
                        mode: isMobile ? 0 : toMode(item.mode),
                      }));
                      status = danmakuLoadedStatus(list.length, (resp as { meta?: DanmakuMeta }).meta);
                    } else {
                      status = danmakuRequestFailedStatus(resp.msg);
                    }
                  } catch (e) {
                    console.error("加载弹幕失败", e);
                    status = danmakuRequestFailedStatus();
                  }
                  showDanmakuStatus(status);
                  // 手机没有输入框：只在失败时用播放器 notice 简短提示一次，其余状态不打扰
                  if (isMobile && status.tone === "error") art.notice.show = status.title;
                }
                listLoaded = true;
                // 刚发出的弹幕被 load 清掉后按当前进度重新飞一次
                const sent = sentBeforeLoad.splice(0);
                return sent.length ? list.concat(sent.map((d) => ({ ...d, time: Math.max(d.time, art.currentTime) }))) : list;
              },
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
                  if (!listLoaded) {
                    sentBeforeLoad.push({
                      text: danmu.text,
                      time: danmu.time || art.currentTime,
                      color: danmu.color || "#ffffff",
                      mode: toMode(danmu.mode),
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
    if (danmakuEnabled) {
      art.on("ready", renderDanmakuStatus);
      // 插件每次改设置都发整份参数，只存和上次相比真正变了的字段
      let lastDanmakuSettings = pickDanmakuSettings((art.plugins as any)?.artplayerPluginDanmuku?.option);
      art.on("artplayerPluginDanmuku:config", (option) => {
        const next = pickDanmakuSettings(option);
        const changed = changedDanmakuSettings(lastDanmakuSettings, next);
        lastDanmakuSettings = next;
        saveDanmakuSettings(changed, isMobile);
      });
      art.on("artplayerPluginDanmuku:show", () => saveDanmakuSettings({ visible: true }, isMobile));
      art.on("artplayerPluginDanmuku:hide", () => saveDanmakuSettings({ visible: false }, isMobile));
      // 拖动进度后清掉屏幕上正在飞的旧弹幕，新位置之后的弹幕由插件按时间照常发出
      art.on("video:seeking", () => {
        (art.plugins as any)?.artplayerPluginDanmuku?.reset();
      });
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

    art.on("video:playing", () => {
      setHasError(false);
      callbacks.current.onPlayStart?.();
    });

    return () => {
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
            icon={<CloseCircleFilled className={styles.resultIconGlyph} />}
            classNames={{
              root: styles.result,
              icon: styles.resultIcon,
              title: styles.resultTitle,
              subTitle: styles.resultSubtitle,
              extra: styles.resultExtra,
            }}
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
