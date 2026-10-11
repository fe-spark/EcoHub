"use client";

import React, { useEffect, useRef, useState } from "react";
import Artplayer from "artplayer";
import Hls from "hls.js";
import { Button, Result } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import styles from "./index.module.less";

interface VideoPlayerProps {
  src: string;
  poster?: string;
  autoplay?: boolean;
  initialTime?: number;
  onEnded?: () => void;
  onTimeUpdate?: (currentTime: number, duration: number) => void;
  onError?: (error: any) => void;
  onPlayStart?: () => void;
}

/**
 * 极简播放器组件：
 * 采用 "src as key" 策略。
 * 核心原则：加载失败即展示 UI 报错与重试按钮，不进行静默处理。
 */
const VideoPlayer: React.FC<VideoPlayerProps> = ({
  src,
  poster,
  autoplay = true,
  initialTime = 0,
  onEnded,
  onTimeUpdate,
  onError,
  onPlayStart,
}) => {
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
    const isMobile = /iPhone|iPad|iPod|Android/i.test(navigator.userAgent) || window.innerWidth <= 768;
    Artplayer.PLAYBACK_RATE = [0.5, 0.75, 1, 1.25, 1.5, 2, 2.5, 3];
    const isM3U8 = src.includes(".m3u8") || src.includes("m3u8");
    const art = new Artplayer({
      container: artRef.current,
      url: src,
      type: isM3U8 ? "m3u8" : undefined,
      poster: poster || "",
      autoplay,
      theme: "#fa8c16", // 直接使用 Hex 色值，Artplayer 内部无法解析 CSS 变量
      volume: 0.7,
      pip: false,
      autoMini: false,
      setting: true,
      playbackRate: true,
      aspectRatio: true,
      fullscreen: true,
      fullscreenWeb: !isMobile,
      mutex: true,
      backdrop: false, // 禁用全屏 dim 遮罩，避免移动端（华为/手机浏览器）全屏变暗及拦截 Header 点击
      playsInline: true,
      // initialTime > 0 时业务层已指定跳转位置，关闭 autoPlayback 避免两套机制冲突；
      // 否则开启，让 Artplayer 以视频 URL 为 key 自动恢复每集各自的播放进度
      autoPlayback: initialTime <= 0,
      airplay: true,
      useSSR: typeof window === "undefined",
      customType: {
        m3u8: function (video: HTMLMediaElement, url: string) {
          if (Hls.isSupported()) {
            if (hlsRef.current) hlsRef.current.destroy();
            const hls = new Hls({
              enableWorker: true,
              maxBufferSize: 60 * 1024 * 1024, // 60MB 缓冲区
              maxBufferLength: 30, // 30s 适中前向缓冲，缩短 seek 时的切片清理和下载等待
              maxMaxBufferLength: 60,
              backBufferLength: 30, // 保留 30s 回退缓冲，极大加快向后跳转速度
              maxBufferHole: 0.5, // 容纳 500ms 缓冲空洞，遇到时间戳微小断层自动跳过
              highBufferWatchdogPeriod: 2,
              nudgeOffset: 0.1, // 遇到渲染停滞微推 0.1s 促使解码器重新捕获关键帧
              nudgeMaxRetry: 5, // 最多微推 5 次跳过坏帧或空洞
              fragLoadingMaxRetry: 5,
              levelLoadingMaxRetry: 5,
              manifestLoadingMaxRetry: 3,
              startLevel: -1, // 自动选择最佳清晰度
              abrEwmaDefaultEstimate: 5000000, // 初始带宽预估 (5Mbps)
              testBandwidth: true, // 加强带宽测试
            });
            hlsRef.current = hls;
            hls.loadSource(url);
            hls.attachMedia(video);

            hls.on(Hls.Events.MANIFEST_PARSED, () => {
              if (initialTime > 0) {
                art.currentTime = initialTime;
              }
            });

            let mediaRecoverTried = false;
            // 监听 HLS 致命错误，只要报错立刻展示 UI 并回调通知上层进行故障转移
            hls.on(Hls.Events.ERROR, (_, data) => {
              if (data.fatal) {
                console.error("HLS Fatal Error:", data);
                if (data.type === Hls.ErrorTypes.MEDIA_ERROR && !mediaRecoverTried) {
                  mediaRecoverTried = true;
                  hls.recoverMediaError();
                } else {
                  try {
                    hls.destroy();
                  } catch (e) {
                    // ignore
                  }
                  setHasError(true);
                  callbacks.current.onError?.(data);
                }
              } else if (data.details === Hls.ErrorDetails.BUFFER_STALLED_ERROR) {
                // 检测到非致命缓冲卡顿（常见于音画时间戳不同步或缺少关键帧）
                console.warn("HLS Buffer Stalled detected, nudging playback pipeline...");
              }
            });
          } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
            video.src = url;
            if (initialTime > 0) {
              const onLoaded = () => {
                art.currentTime = initialTime;
                video.removeEventListener("loadedmetadata", onLoaded);
              };
              video.addEventListener("loadedmetadata", onLoaded);
            }
          }
          // 对移动端进行标准行内播放标签补全
          video.setAttribute("playsinline", "true");
          video.setAttribute("webkit-playsinline", "true");
          if (/MicroMessenger/i.test(navigator.userAgent)) {
            video.setAttribute("x5-video-player-type", "h5");
            video.setAttribute("x5-video-player-fullscreen", "true");
          }
        },
      },
    });

    if (art.template?.$video) {
      const v = art.template.$video;
      v.setAttribute("playsinline", "true");
      v.setAttribute("webkit-playsinline", "true");
      v.setAttribute("x5-playsinline", "true");
      v.setAttribute("x5-video-player-type", "h5");
      v.setAttribute("x5-video-player-fullscreen", "false");
    }

    playerRef.current = art;

    // 小窗触发：用 IntersectionObserver + 滚动监听，避免 rAF 常驻循环占 CPU
    let miniRafId = 0;
    let onScrollOrResize: (() => void) | null = null;
    let miniObserver: IntersectionObserver | null = null;
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
      if (art.template?.$video) {
        const v = art.template.$video;
        v.setAttribute("playsinline", "true");
        v.setAttribute("webkit-playsinline", "true");
        v.setAttribute("x5-playsinline", "true");
      }
      if (initialTime > 0 && !isM3U8) {
        art.currentTime = initialTime;
      }
    });

    art.on("video:ended", () => callbacks.current.onEnded?.());
    art.on("video:timeupdate", () =>
      callbacks.current.onTimeUpdate?.(art.currentTime, art.duration),
    );

    // 监听播放器通用错误
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
    let seekEndTimer: any = null;

    art.on("video:seeking", () => {
      isSeeking = true;
      const v = art.template?.$video;
      if (v && !v.paused) {
        wasPlayingBeforeSeek = true;
        v.pause();
      }
      if (art.loading) {
        art.loading.show = true;
      }
    });

    const resumePlaybackAfterSeek = () => {
      if (isSeeking) {
        isSeeking = false;
        if (art.loading) {
          art.loading.show = false;
        }
        if (wasPlayingBeforeSeek) {
          wasPlayingBeforeSeek = false;
          art.play().catch(() => {});
        }
      }
    };

    art.on("video:canplay", () => {
      if (isSeeking) {
        clearTimeout(seekEndTimer);
        resumePlaybackAfterSeek();
      }
    });

    art.on("video:seeked", () => {
      const v = art.template?.$video;
      if (v && v.readyState >= 3) {
        resumePlaybackAfterSeek();
      } else {
        clearTimeout(seekEndTimer);
        seekEndTimer = setTimeout(resumePlaybackAfterSeek, 2500);
      }
    });

    art.on("video:playing", () => {
      setHasError(false);
      callbacks.current.onPlayStart?.();
    });

    // 移动端全屏事件接管：针对 iOS 等不支持标准全屏 API 的设备，调用原生 video.webkitEnterFullscreen
    art.on("fullscreen", (state) => {
      if (state && typeof document.body.requestFullscreen === "undefined") {
        const video = art.template.$video as any;
        if (video.webkitEnterFullscreen) {
          video.webkitEnterFullscreen();
        }
      }
    });

    return () => {
      if (seekEndTimer) clearTimeout(seekEndTimer);
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
      if (art) {
        if (art.mini) art.mini = false;
        art.destroy(true);
      }
      playerRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [retryCount, src]);

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
