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
  changedDanmakuSettings,
  loadDanmakuSettings,
  pickDanmakuSettings,
  saveDanmakuSettings,
  type DanmakuSettings,
} from "./danmakuSettings";
import {
  DEFAULT_FONT_SIZE,
  DEFAULT_MARGIN,
  bindDanmakuLayout,
  computeDanmakuLayout,
  smallPlayerScrollOnly,
  type DanmakuBase,
} from "./danmakuLayout";
import { WIDE_FROM, bindPlayerWidthClass, currentPlayerWidthClass } from "./playerLayout";
import {
  DANMAKU_LOADING_STATUS,
  renderDanmakuStatus as renderDanmakuStatusDom,
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
  /** 视频真实分辨率可用或变化时回调（loadedmetadata / resize，例如 HLS 切换清晰度）。 */
  onVideoSize?: (width: number, height: number) => void;
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
  onVideoSize,
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
  const callbacks = useRef({ onEnded, onTimeUpdate, onError, onPlayStart, onVideoSize });
  useEffect(() => {
    callbacks.current = { onEnded, onTimeUpdate, onError, onPlayStart, onVideoSize };
  }, [onEnded, onTimeUpdate, onError, onPlayStart, onVideoSize]);

  // 核心初始化 Effect
  useEffect(() => {
    let player: Artplayer | null = null;
    let miniRafId = 0;
    let onScrollOrResize: (() => void) | null = null;
    let miniObserver: IntersectionObserver | null = null;

    if (!artRef.current) return;

    Artplayer.PLAYBACK_RATE = [0.5, 0.75, 1, 1.25, 1.5, 2, 2.5, 3];
    const isM3U8 = src.includes(".m3u8") || src.includes("m3u8");
    type DanmakuMode = 0 | 1 | 2;
    const toMode = (m?: number): DanmakuMode => (m === 1 ? 1 : m === 2 ? 2 : 0);
    // 弹幕列表返回前用户已发出的弹幕：插件 load 完会清空队列，这里记下来随列表一起装回去
    const sentBeforeLoad: { text: string; time: number; color: string; mode: DanmakuMode }[] = [];
    let listLoaded = false;
    // 弹幕字号和显示区域：保存的是用户设定（字号按大播放器计，显示区域相对画面），
    // 实际传给插件的值按播放器尺寸换算，尺寸变化时重算（见 danmakuLayout.ts）
    const savedDanmaku = loadDanmakuSettings();
    const danmakuBase: DanmakuBase = {
      fontSize: savedDanmaku.fontSize ?? DEFAULT_FONT_SIZE,
      margin: savedDanmaku.margin ?? DEFAULT_MARGIN,
    };
    const initialDanmakuLayout = computeDanmakuLayout(
      danmakuBase,
      artRef.current.clientWidth,
      artRef.current.clientHeight,
    );
    // 弹幕加载状态画在控制条上，不用 notice 盖在画面上：输入框显示时在输入框（灰字 + 状态点），
    // 窄播放器收起输入框时在弹幕开关上（状态点），见 danmakuStatus.ts / playerLayout.ts。
    // 插件控件可能晚于本函数挂上，记下最新状态，ready 时再补一次。
    let danmakuStatus: DanmakuStatus | null = null;
    const renderDanmakuStatus = () => renderDanmakuStatusDom(art?.template?.$player, danmakuStatus);
    const showDanmakuStatus = (status: DanmakuStatus) => {
      danmakuStatus = status;
      renderDanmakuStatus();
      // 输入框收起时状态点很小：失败再用播放器 notice 简短提示一次，其余状态不打扰
      if (status.tone === "error" && currentPlayerWidthClass(art) === "compact") art.notice.show = status.title;
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
      // 网页全屏和弹幕输入框都创建，窄播放器上由样式按宽度收起（playerLayout.ts）
      fullscreenWeb: true,
      mutex: true,
      backdrop: false,
      playsInline: true,
      autoPlayback: initialTime <= 0,
      airplay: true,
      useSSR: typeof window === "undefined",
      plugins: danmakuEnabled
        ? [
            artplayerPluginDanmuku({
              ...savedDanmaku,
              fontSize: initialDanmakuLayout.fontSize,
              margin: initialDanmakuLayout.margin,
              // 插件类型声明把档位值写成 string | number，实际是 [上边距, 下边距]
              MARGIN: initialDanmakuLayout.MARGIN as never,
              // 中、窄播放器上固定弹幕按滚动显示，和滚动弹幕一起避让
              beforeVisible: smallPlayerScrollOnly(() => art?.template?.$player),
              synchronousPlayback: true,
              // 不按播放器宽度把弹幕栏挪到播放器外面（外层容器 overflow:hidden 会把它裁掉）
              width: 0,
              emitter: true,
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
                        mode: toMode(item.mode),
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
    // 本组件的播放器样式挂在根节点上（网页全屏时根节点会被移出外层容器）
    art.template.$player?.classList.add(styles.artPlayer);
    // 按播放器显示宽度切换控制条排版（输入框、网页全屏按钮、状态显示位置），尺寸变化时实时切换
    const unbindWidthClass = bindPlayerWidthClass(art, () => renderDanmakuStatus());
    if (danmakuEnabled) {
      art.on("ready", renderDanmakuStatus);
      // 插件每次改设置都发整份参数：只存用户真正改过的字段；按尺寸换算出来的字号、显示区域不存
      bindDanmakuLayout(
        art,
        danmakuBase,
        (changed) => saveDanmakuSettings(changed as DanmakuSettings),
        (option) => pickDanmakuSettings(option) as Record<string, unknown>,
        (prev, next) => changedDanmakuSettings(prev as DanmakuSettings, next as DanmakuSettings) as Record<string, unknown>,
      );
      art.on("artplayerPluginDanmuku:show", () => saveDanmakuSettings({ visible: true }));
      art.on("artplayerPluginDanmuku:hide", () => saveDanmakuSettings({ visible: false }));
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

    if (artRef.current) {
      const updateMiniState = () => {
        if (!artRef.current || !art) return;
        // 小窗只在宽屏页面上用：窄窗口、手机上小窗会盖住大半页面。按当前窗口宽度实时判断
        if (art.fullscreen || art.fullscreenWeb || window.innerWidth < WIDE_FROM) {
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

    // 把视频真实宽高报给外层，用来按「撑满宽度」算播放区高度。
    // 直接监听原生 video 事件：resize 在 HLS 切换清晰度、分辨率变化时也会触发。
    const reportVideoSize = () => {
      const { videoWidth, videoHeight } = art.video;
      if (videoWidth > 0 && videoHeight > 0) {
        callbacks.current.onVideoSize?.(videoWidth, videoHeight);
      }
    };
    art.video.addEventListener("loadedmetadata", reportVideoSize);
    art.video.addEventListener("resize", reportVideoSize);

    art.on("video:playing", () => {
      setHasError(false);
      callbacks.current.onPlayStart?.();
    });

    return () => {
      art.video.removeEventListener("loadedmetadata", reportVideoSize);
      art.video.removeEventListener("resize", reportVideoSize);
      unbindWidthClass();
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
