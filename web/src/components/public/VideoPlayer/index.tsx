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
}) => {
  const artRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<Artplayer | null>(null);
  const hlsRef = useRef<Hls | null>(null);
  const [hasError, setHasError] = useState(false);
  const [retryCount, setRetryCount] = useState(0);

  // 回调保护
  const callbacks = useRef({ onEnded, onTimeUpdate, onError });
  useEffect(() => {
    callbacks.current = { onEnded, onTimeUpdate, onError };
  }, [onEnded, onTimeUpdate, onError]);

  // 核心初始化 Effect
  useEffect(() => {
    const isMobile = /iPhone|iPad|iPod|Android/i.test(navigator.userAgent) || window.innerWidth <= 768;
    Artplayer.PLAYBACK_RATE = [0.5, 0.75, 1, 1.25, 1.5, 2, 2.5, 3];
    const art = new Artplayer({
      container: artRef.current,
      url: src,
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
      backdrop: true,
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
              maxBufferSize: 100 * 1024 * 1024, // 100MB 缓冲区
              maxBufferLength: 60, // 增加预加载时长到 60s
              fragLoadingMaxRetry: 10, // 增加切片下载重载次数
              levelLoadingMaxRetry: 10,
              manifestLoadingMaxRetry: 10,
              startLevel: -1, // 自动选择最佳清晰度
              abrEwmaDefaultEstimate: 5000000, // 初始带宽预估 (5Mbps)
              testBandwidth: true, // 加强带宽测试
            });
            hlsRef.current = hls;
            hls.loadSource(url);
            hls.attachMedia(video);

            // 监听 HLS 致命错误，只要报错立刻展示 UI
            hls.on(Hls.Events.ERROR, (_, data) => {
              if (data.fatal) {
                console.error("HLS Fatal Error:", data);
                // 忽略非关键错误，尝试自动恢复
                if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
                  hls.startLoad();
                } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
                  hls.recoverMediaError();
                } else {
                  setHasError(true);
                  callbacks.current.onError?.(data);
                }
              }
            });
          } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
            video.src = url;
          }
          // 对移动端进行原生标签补全
          video.setAttribute("playsinline", "true");
          video.setAttribute("webkit-playsinline", "true");
          video.setAttribute("x5-video-player-type", "h5");
          video.setAttribute("x5-video-player-fullscreen", "true");
        },
      },
    });

    playerRef.current = art;

    // 破解 autoMini 延迟：实现毫秒级响应的小窗触发
    let rafId: number;
    if (!isMobile) {
      const checkMiniTrigger = () => {
        if (!artRef.current || !art) return;

        // 全屏场景下禁用小窗切换
        if (art.fullscreen || art.fullscreenWeb) {
          if (art.mini) art.mini = false;
          rafId = requestAnimationFrame(checkMiniTrigger);
          return;
        }

        const rect = artRef.current.getBoundingClientRect();
        // 阈值破解：底部剩余 100px 时立即开启，顶部进入 -50px 时立刻关闭
        if (rect.bottom < 100) {
          if (art.playing && !art.mini) art.mini = true;
        } else if (rect.top > -50) {
          if (art.mini) art.mini = false;
        }
        rafId = requestAnimationFrame(checkMiniTrigger);
      };
      rafId = requestAnimationFrame(checkMiniTrigger);
    }

    art.on("ready", () => {
      if (initialTime > 0) {
        art.currentTime = initialTime;
      }
    });

    art.on("video:ended", () => callbacks.current.onEnded?.());
    art.on("video:timeupdate", () =>
      callbacks.current.onTimeUpdate?.(art.currentTime, art.duration),
    );

    // 监听播放器通用错误
    art.on("error", (err) => {
      console.error("Artplayer Error:", err);
      setHasError(true);
      callbacks.current.onError?.(err);
    });

    art.on("video:playing", () => setHasError(false));

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
      if (rafId) cancelAnimationFrame(rafId);
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
