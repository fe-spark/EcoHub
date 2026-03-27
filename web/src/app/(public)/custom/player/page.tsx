"use client";

import React, { useState, useRef, useEffect } from "react";
import { Input, Button, message, Spin } from "antd";
import { PlayCircleOutlined, SearchOutlined } from "@ant-design/icons";
import videojs from "video.js";
import "video.js/dist/video-js.css";
import styles from "./page.module.less";

export default function CustomPlayPage() {
  const [url, setUrl] = useState("");
  const [currentUrl, setCurrentUrl] = useState("");
  const videoRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<any>(null);

  const handlePlay = () => {
    const pattern =
      /(^http[s]?:\/\/[^\s]+\.m3u8$)|(^http[s]?:\/\/[^\s]+\.mp4$)/;
    if (!pattern.test(url)) {
      message.error("视频链接格式异常, 请输入正确的 .mp4 或 .m3u8 链接");
      return;
    }
    setCurrentUrl(url);
  };

  useEffect(() => {
    if (!currentUrl || !videoRef.current) return;

    if (!playerRef.current) {
      const videoElement = document.createElement("video-js");
      videoElement.classList.add("vjs-big-play-button-centered");
      videoRef.current.appendChild(videoElement);

      playerRef.current = videojs(videoElement, {
        autoplay: true,
        controls: true,
        responsive: true,
        fluid: true,
        sources: [
          {
            src: currentUrl,
            type: currentUrl.includes(".m3u8")
              ? "application/x-mpegURL"
              : "video/mp4",
          },
        ],
      });
    } else {
      playerRef.current.src({
        src: currentUrl,
        type: currentUrl.includes(".m3u8")
          ? "application/x-mpegURL"
          : "video/mp4",
      });
      playerRef.current.play();
    }
  }, [currentUrl]);

  useEffect(() => {
    return () => {
      if (playerRef.current) {
        playerRef.current.dispose();
      }
    };
  }, []);

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <div className={styles.searchBar}>
          <Input
            placeholder="请输入视频播放地址, mp4 或 m3u8 格式"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onPressEnter={handlePlay}
            size="large"
            suffix={
              <Button
                type="text"
                icon={<SearchOutlined />}
                onClick={handlePlay}
              />
            }
          />
        </div>
      </div>

      <div className={styles.playerArea}>
        {currentUrl ? (
          <div className={styles.videoWrapper} ref={videoRef} />
        ) : (
          <div className={styles.placeholder}>
            <PlayCircleOutlined
              style={{ fontSize: 64, color: "rgba(255,255,255,0.1)" }}
            />
            <p>输入链接并点击播放</p>
          </div>
        )}
      </div>
    </div>
  );
}
