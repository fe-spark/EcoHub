"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Button, Tour } from "antd";
import type { TourProps } from "antd";
import styles from "./index.module.less";

const STORAGE_KEY = "ecohub_manage_tour_done_v2";
const REPLAY_EVENT = "ecohub:replay-manage-tour";
const PROGRESS_TARGET = "[data-tour='collect-progress']";
/** 引导停在采集步时通知采集页：进度条不要倒计时收起 */
export const TOUR_HOLD_PROGRESS_ATTR = "data-tour-hold-progress";
export const COLLECT_BATCH_FAILED_EVENT = "ecohub:collect-batch-failed";

type GuideStep = {
  title: string;
  description: string;
  route: string;
  target: string | null;
  placement?: "top" | "bottom" | "left" | "right" | "rightTop" | "bottomRight";
  /** 必须先点这个业务按钮 */
  requireClick?: string;
  /** 批量弹窗里的确认按钮，需在 requireClick 之后再点 */
  submitClick?: string;
  /** 进度条采集结束后才能下一步 */
  waitMasterDone?: boolean;
};

const GUIDE_STEPS: GuideStep[] = [
  {
    title: "先把采集走通",
    description:
      "首次启动已内置主站和多个附属站。片库要自己采：主站和附属站可同时采，发布会等主站采完；然后检查分类/规则，再打开自动更新。",
    route: "/manage/collect",
    target: "[data-tour='menu-collect']",
    placement: "right",
  },
  {
    title: "全选采集站",
    description:
      "把内置主站和附属站都勾上。后面批量启用、批量采集都基于这次选择。",
    route: "/manage/collect",
    target: "[data-tour='collect-select-all']",
    placement: "bottom",
    requireClick: "[data-tour='collect-select-all']",
  },
  {
    title: "批量启用",
    description:
      "选中的站需要先启用才能采。没启用的站批量采集会跳过。",
    route: "/manage/collect",
    target: "[data-tour='collect-batch-enable']",
    placement: "bottom",
    requireClick: "[data-tour='collect-batch-enable']",
  },
  {
    title: "批量采集",
    description:
      "主站和附属站可同时采，发布会等主站采完。开始后可看进度和终止。",
    route: "/manage/collect",
    target: "[data-tour='collect-batch']",
    placement: "bottom",
    requireClick: "[data-tour='collect-batch']",
    submitClick: "[data-tour='collect-batch-submit']",
    waitMasterDone: true,
  },
  {
    title: "分类管理",
    description:
      "分类不能删，只能隐藏或显示，并调整排序。",
    route: "/manage/collect/category",
    target: "[data-tour='menu-category']",
    placement: "right",
  },
  {
    title: "分类规则",
    description:
      "把源站原始分类合并到前台展示分类。主站分类乱、重名或过细时在这里映射。",
    route: "/manage/collect/category/rules",
    target: "[data-tour='menu-rules']",
    placement: "right",
  },
  {
    title: "计划任务",
    description:
      "打开自动更新和失败重试，之后不用天天手点采集。改运行时间前先确认任务已启用。",
    route: "/manage/cron",
    target: "[data-tour='menu-cron']",
    placement: "right",
  },
  {
    title: "失败记录",
    description:
      "单源失败、重试次数和最终失败都在这里。采集中断先看记录，再决定重采或换源。",
    route: "/manage/collect/record",
    target: "[data-tour='menu-record']",
    placement: "right",
  },
  {
    title: "核对影片列表",
    description:
      "快照发布后到这里搜主站影片。没有数据通常是采集未完成，或快照还没发布完。",
    route: "/manage/film",
    target: "[data-tour='menu-film']",
    placement: "right",
  },
];

type ProgressPhase = "running" | "done" | "failed" | "stopped";

function readProgressPhase(): ProgressPhase | null {
  const el = queryTarget(PROGRESS_TARGET);
  const phase = el?.getAttribute("data-tour-progress");
  if (
    phase === "running" ||
    phase === "done" ||
    phase === "failed" ||
    phase === "stopped"
  ) {
    return phase;
  }
  return null;
}

function collectWaitDescription(phase: ProgressPhase | null, ended: boolean) {
  if (phase === "running") {
    return "采集进行中，结束后可进入下一步。";
  }
  if (ended || phase === "done" || phase === "failed" || phase === "stopped") {
    return "采集已结束，可以进入下一步。";
  }
  return "开始采集后可看顶部进度；采集结束后即可进入下一步。";
}

function readDone() {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === "1";
  } catch {
    return true;
  }
}

function markDone() {
  try {
    window.localStorage.setItem(STORAGE_KEY, "1");
  } catch {
    /* ignore */
  }
}

function queryTarget(selector: string | null) {
  if (!selector || typeof document === "undefined") {
    return null;
  }
  return document.querySelector<HTMLElement>(selector);
}

function isMenuTarget(target: string | null) {
  return Boolean(target?.includes("menu-"));
}

export default function ManageTour({
  isMobile,
  canWrite = true,
  permissionReady = true,
  onNeedMenu,
}: {
  isMobile: boolean;
  canWrite?: boolean;
  permissionReady?: boolean;
  onNeedMenu?: (need: boolean) => void;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const [open, setOpen] = useState(false);
  const [current, setCurrent] = useState(0);
  const [clicked, setClicked] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [progressEnded, setProgressEnded] = useState(false);
  const [targetTick, setTargetTick] = useState(0);
  const [modalOpen, setModalOpen] = useState(false);
  const [progressPhase, setProgressPhase] = useState<ProgressPhase | null>(null);
  const queuedRef = useRef<number | null>(null);
  /** 本轮必须先见到进度条 running，才认 done，避免上一轮结束态误开下一步 */
  const sawRunningRef = useRef(false);

  const step = GUIDE_STEPS[current];
  const needClick = Boolean(step?.requireClick);
  const needSubmit = Boolean(step?.submitClick);
  const needWait = Boolean(step?.waitMasterDone);
  const skipActions = !canWrite;
  const canNext = skipActions
    ? true
    : (!needClick || clicked) &&
      (!needSubmit || submitted) &&
      (!needWait || (submitted && progressEnded));

  const onNeedMenuRef = useRef(onNeedMenu);
  onNeedMenuRef.current = onNeedMenu;
  const pathnameRef = useRef(pathname);
  pathnameRef.current = pathname;
  const isMobileRef = useRef(isMobile);
  isMobileRef.current = isMobile;

  const syncMenuForTarget = useCallback((target: string | null) => {
    const need = isMenuTarget(target);
    if (isMobileRef.current) {
      onNeedMenuRef.current?.(need);
      return;
    }
    if (need) {
      onNeedMenuRef.current?.(true);
    }
  }, []);

  const showStep = useCallback(
    (index: number) => {
      const next = GUIDE_STEPS[index];
      if (!next) {
        return;
      }
      syncMenuForTarget(next.target);
      queuedRef.current = index;
      setClicked(false);
      setSubmitted(false);
      setProgressEnded(false);
      sawRunningRef.current = false;
      if (pathnameRef.current !== next.route) {
        setOpen(false);
        router.push(next.route);
        return;
      }
      window.requestAnimationFrame(() => {
        if (queuedRef.current !== index) {
          return;
        }
        setCurrent(index);
        setOpen(true);
      });
    },
    [router, syncMenuForTarget],
  );

  useEffect(() => {
    const index = queuedRef.current;
    if (index == null) {
      return;
    }
    const next = GUIDE_STEPS[index];
    if (!next || pathname !== next.route) {
      return;
    }
    syncMenuForTarget(next.target);
    let tries = 0;
    const timer = window.setInterval(() => {
      tries += 1;
      if (!next.target || queryTarget(next.target) || tries > 40) {
        window.clearInterval(timer);
        setCurrent(index);
        setOpen(true);
      }
    }, 50);
    return () => window.clearInterval(timer);
  }, [pathname, syncMenuForTarget]);

  useEffect(() => {
    const replay = () => showStep(0);
    window.addEventListener(REPLAY_EVENT, replay);
    return () => window.removeEventListener(REPLAY_EVENT, replay);
  }, [showStep]);

  useEffect(() => {
    const onFail = () => {
      sawRunningRef.current = false;
      setSubmitted(false);
      setProgressEnded(false);
    };
    window.addEventListener(COLLECT_BATCH_FAILED_EVENT, onFail);
    return () => window.removeEventListener(COLLECT_BATCH_FAILED_EVENT, onFail);
  }, []);

  useEffect(() => {
    if (!permissionReady || !canWrite || readDone()) {
      return;
    }
    const timer = window.setTimeout(() => showStep(0), 400);
    return () => window.clearTimeout(timer);
  }, [canWrite, permissionReady, showStep]);

  useEffect(() => {
    if (!open || (!step?.requireClick && !step?.submitClick)) {
      return;
    }
    const onClick = (event: MouseEvent) => {
      const el = event.target;
      if (!(el instanceof Element)) {
        return;
      }
      if (step.requireClick && el.closest(step.requireClick)) {
        setClicked(true);
      }
      if (step.submitClick && el.closest(step.submitClick)) {
        sawRunningRef.current = false;
        setSubmitted(true);
        setProgressEnded(false);
      }
    };
    document.addEventListener("click", onClick, true);
    return () => document.removeEventListener("click", onClick, true);
  }, [open, step]);

  useEffect(() => {
    if (!open) {
      return;
    }
    const selectors = [
      step?.target,
      step?.requireClick,
      step?.submitClick,
      PROGRESS_TARGET,
    ].filter((item): item is string => Boolean(item));
    let prev = `${selectors.map((item) => Boolean(queryTarget(item))).join(",")},${readProgressPhase()}`;
    const check = () => {
      const phase = readProgressPhase();
      setModalOpen(Boolean(step?.submitClick && queryTarget(step.submitClick)));
      setProgressPhase(phase);
      const next = `${selectors.map((item) => Boolean(queryTarget(item))).join(",")},${phase}`;
      if (next !== prev) {
        prev = next;
        setTargetTick((n) => n + 1);
      }
      if (!submitted || !step?.waitMasterDone) {
        return;
      }
      if (phase === "running") {
        sawRunningRef.current = true;
        setProgressEnded(false);
        return;
      }
      if (!sawRunningRef.current) {
        return;
      }
      if (phase === "done" || phase === "failed" || phase === "stopped") {
        setProgressEnded(true);
      }
    };
    const obs = new MutationObserver(check);
    obs.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ["data-tour-progress"],
    });
    check();
    return () => obs.disconnect();
  }, [open, step, submitted]);

  useEffect(() => {
    const hold = Boolean(open && needWait && submitted);
    if (hold) {
      document.documentElement.setAttribute(TOUR_HOLD_PROGRESS_ATTR, "1");
    } else {
      document.documentElement.removeAttribute(TOUR_HOLD_PROGRESS_ATTR);
    }
    return () => {
      document.documentElement.removeAttribute(TOUR_HOLD_PROGRESS_ATTR);
    };
  }, [open, needWait, submitted]);

  const closeTour = () => {
    queuedRef.current = null;
    markDone();
    setOpen(false);
  };

  const watchingProgress = Boolean(needWait && submitted);
  const liveTarget = modalOpen
    ? (step?.submitClick ?? null)
    : watchingProgress
      ? PROGRESS_TARGET
      : (step?.target ?? null);

  const currentStep: NonNullable<TourProps["steps"]>[number] = {
    title: step?.title,
    placement: watchingProgress && !modalOpen ? "top" : step?.placement,
    target: () => queryTarget(liveTarget),
    description: watchingProgress
      ? collectWaitDescription(progressPhase, progressEnded)
      : step?.description,
  };

  const last = current >= GUIDE_STEPS.length - 1;

  return (
    <Tour
      key={`${pathname}-${current}-${modalOpen ? "modal" : watchingProgress ? "progress" : "page"}-${targetTick}`}
      open={open}
      current={0}
      onClose={closeTour}
      steps={[currentStep]}
      zIndex={1100}
      classNames={{ section: styles.panel }}
      styles={{
        section: {
          border: "1px solid var(--ant-color-primary)",
          boxShadow: "0 8px 24px rgba(0, 0, 0, 0.45)",
          background: "var(--ant-color-bg-layout)",
        },
      }}
      mask={watchingProgress && !modalOpen ? false : { color: "rgba(0,0,0,0.45)" }}
      actionsRender={() => [
        <Button key="skip" size="small" onClick={closeTour}>
          跳过
        </Button>,
        <Button
          key="next"
          type="primary"
          size="small"
          disabled={!canNext}
          onClick={() => {
            if (last) {
              closeTour();
              return;
            }
            showStep(current + 1);
          }}
        >
          {last
            ? "完成"
            : watchingProgress && !progressEnded && progressPhase === "running"
              ? "等待采集完成"
              : "下一步"}
        </Button>,
      ]}
    />
  );
}

export function replayManageTour() {
  window.dispatchEvent(new Event(REPLAY_EVENT));
}
