"use client";

import React from "react";
import { Spin } from "antd";

interface AppLoadingProps {
  text?: string;
  padding?: string;
  size?: "small" | "default" | "large";
}

export default function AppLoading({
  text,
  padding,
  size = "large",
}: AppLoadingProps) {
  const loadingText = text ?? "加载中...";
  const indicatorSize = size === "small" ? 28 : size === "large" ? 46 : 36;

  const indicator = (
    <span
      style={{
        display: "inline-flex",
        width: indicatorSize,
        height: indicatorSize,
        animation: "app-loading-spin 0.9s linear infinite",
      }}
    >
      <svg
        viewBox="0 0 50 50"
        width={indicatorSize}
        height={indicatorSize}
        aria-hidden="true"
      >
        <circle
          cx="25"
          cy="25"
          r="20"
          fill="none"
          stroke="rgba(var(--primary-color-rgb), 0.25)"
          strokeWidth="5"
        />
        <path
          d="M25 5 A20 20 0 0 1 45 25"
          fill="none"
          stroke="var(--primary-color)"
          strokeWidth="5"
          strokeLinecap="round"
        />
      </svg>
    </span>
  );

  const containerStyle: React.CSSProperties = padding
    ? {
        padding,
        textAlign: "center",
      }
    : {
        position: "absolute",
        left: 0,
        top: 0,
        width: "100%",
        height: "100%",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        flexDirection: "column",
        gap: "12px",
      };

  return (
    <div style={containerStyle}>
      <style>{`@keyframes app-loading-spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }`}</style>
      <Spin size={size} indicator={indicator} />
      {loadingText ? (
        <div style={{ color: "var(--public-text-2)", fontSize: "15px" }}>
          {loadingText}
        </div>
      ) : null}
    </div>
  );
}
