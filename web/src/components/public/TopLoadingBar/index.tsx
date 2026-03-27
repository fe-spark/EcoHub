"use client";

import { useEffect } from "react";
import styles from "./index.module.css";

interface TopLoadingBarProps {
  label?: string;
  finishOnWindowLoad?: boolean;
}

let hostEl: HTMLDivElement | null = null;
let barEl: HTMLSpanElement | null = null;
let srOnlyEl: HTMLSpanElement | null = null;
let activeCount = 0;
let progress = 0;
let progressTimer = 0;
let finishTimer = 0;
let hideTimer = 0;

function clearTimers() {
  window.clearInterval(progressTimer);
  window.clearTimeout(finishTimer);
  window.clearTimeout(hideTimer);
  progressTimer = 0;
  finishTimer = 0;
  hideTimer = 0;
}

function ensureElements(label: string) {
  if (!hostEl) {
    hostEl = document.createElement("div");
    hostEl.className = styles.container;
    hostEl.setAttribute("aria-hidden", "true");
    hostEl.style.opacity = "0";

    barEl = document.createElement("span");
    barEl.className = styles.bar;
    hostEl.appendChild(barEl);

    document.body.appendChild(hostEl);
  }

  if (!srOnlyEl) {
    srOnlyEl = document.createElement("span");
    srOnlyEl.className = styles.srOnly;
    document.body.appendChild(srOnlyEl);
  }

  srOnlyEl.textContent = label;
}

function renderProgress(nextProgress: number) {
  progress = nextProgress;
  if (barEl) {
    barEl.style.width = `${nextProgress}%`;
  }
}

function showBar(label: string) {
  ensureElements(label);
  clearTimers();
  renderProgress(12);

  if (hostEl) {
    hostEl.style.opacity = "1";
  }

  progressTimer = window.setInterval(() => {
    if (progress >= 90) {
      window.clearInterval(progressTimer);
      progressTimer = 0;
      return;
    }

    const step = progress < 45 ? 12 : progress < 72 ? 7 : 4;
    renderProgress(Math.min(progress + step, 90));
  }, 90);
}

function finishBar() {
  if (!hostEl || !barEl) {
    return;
  }

  clearTimers();
  renderProgress(100);

  finishTimer = window.setTimeout(() => {
    if (hostEl) {
      hostEl.style.opacity = "0";
    }
  }, 180);

  hideTimer = window.setTimeout(() => {
    renderProgress(0);
  }, 360);
}

function startLoading(label: string) {
  activeCount += 1;
  showBar(label);
}

function stopLoading() {
  activeCount = Math.max(0, activeCount - 1);
  if (activeCount === 0) {
    finishBar();
  }
}

export default function TopLoadingBar({
  label = "页面加载中",
  finishOnWindowLoad = false,
}: TopLoadingBarProps) {
  useEffect(() => {
    startLoading(label);

    if (!finishOnWindowLoad) {
      return () => {
        stopLoading();
      };
    }

    const handleWindowLoad = () => {
      stopLoading();
    };

    if (document.readyState === "complete") {
      handleWindowLoad();
      return;
    }

    window.addEventListener("load", handleWindowLoad, { once: true });

    return () => {
      window.removeEventListener("load", handleWindowLoad);
      stopLoading();
    };
  }, [finishOnWindowLoad, label]);

  return null;
}
