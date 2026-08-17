"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Button, Tour } from "antd";
import type { TourProps } from "antd";
import { ApiGet } from "@/lib/client-api";
import { isActiveCollectStatus } from "@/app/manage/collect/view/types";

const STORAGE_KEY = "ecohub_manage_tour_done_v2";
const REPLAY_EVENT = "ecohub:replay-manage-tour";
const PROGRESS_TARGET = "[data-tour='collect-progress']";

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
  /** 主站采集结束（含发布）后才能下一步 */
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
      "点「全选」，把内置主站和附属站都勾上。后面批量启用、批量采集都基于这次选择。",
    route: "/manage/collect",
    target: "[data-tour='collect-select-all']",
    placement: "bottom",
    requireClick: "[data-tour='collect-select-all']",
  },
  {
    title: "批量启用",
    description:
      "点「批量启用」，保证选中的站都能采。没启用的站批量采集会跳过。",
    route: "/manage/collect",
    target: "[data-tour='collect-batch-enable']",
    placement: "bottom",
    requireClick: "[data-tour='collect-batch-enable']",
  },
  {
    title: "批量采集",
    description:
      "点「批量采集」，在弹窗里确认后点「开始采集」。开始后可看进度和终止；主站采完并发布后才能进入下一步。",
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
      "主站采完、分类树同步后点这里。分类不能删，只能隐藏或显示，并调整排序。",
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

type CollectListItem = {
  grade?: number;
  progress?: { status?: string } | null;
};

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
  if (!selector) {
    return null;
  }
  return document.querySelector<HTMLElement>(selector);
}

function isMenuTarget(target: string | null) {
  return Boolean(target?.includes("menu-"));
}

function resolveMaster(items: CollectListItem[]) {
  const master = items.find((item) => Number(item.grade) === 0);
  const status = master?.progress?.status;
  return {
    status,
    active: isActiveCollectStatus(status),
    done: status === "done",
    failed: status === "failed" || status === "stopped",
  };
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
  const [seenDone, setSeenDone] = useState(false);
  const [targetTick, setTargetTick] = useState(0);
  const [master, setMaster] = useState(resolveMaster([]));
  const queuedRef = useRef<number | null>(null);

  const step = GUIDE_STEPS[current];
  const needClick = Boolean(step?.requireClick);
  const needSubmit = Boolean(step?.submitClick);
  const needWait = Boolean(step?.waitMasterDone);
  const sessionDone = seenDone || master.done;
  const skipActions = !canWrite;
  const canNext = skipActions
    ? true
    : (!needClick || clicked) &&
      (!needSubmit || submitted) &&
      (!needWait || (submitted && sessionDone && !master.active && !master.failed));

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
      setSeenDone(false);
      if (index === 0) {
        setMaster(resolveMaster([]));
      }
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
        setSubmitted(true);
        setSeenDone(false);
      }
    };
    document.addEventListener("click", onClick, true);
    return () => document.removeEventListener("click", onClick, true);
  }, [open, step]);

  useEffect(() => {
    if (!open) {
      return;
    }
    const selectors = [step?.target, step?.requireClick, step?.submitClick, PROGRESS_TARGET].filter(
      (item): item is string => Boolean(item),
    );
    let prev = selectors.map((item) => Boolean(queryTarget(item))).join(",");
    const check = () => {
      const next = selectors.map((item) => Boolean(queryTarget(item))).join(",");
      if (next !== prev) {
        prev = next;
        setTargetTick((n) => n + 1);
      }
    };
    const obs = new MutationObserver(check);
    obs.observe(document.body, { childList: true, subtree: true });
    check();
    return () => obs.disconnect();
  }, [open, step]);

  useEffect(() => {
    if (!open || !step?.waitMasterDone || !submitted) {
      return;
    }
    let cancelled = false;
    const pull = async () => {
      try {
        const resp = await ApiGet<CollectListItem[]>("/manage/collect/list");
        if (!cancelled && resp.code === 0 && Array.isArray(resp.data)) {
          const next = resolveMaster(resp.data);
          setMaster(next);
          if (next.done) {
            setSeenDone(true);
          }
        }
      } catch {
        /* ignore */
      }
    };
    void pull();
    const timer = window.setInterval(() => {
      void pull();
    }, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [open, step, submitted]);

  const closeTour = () => {
    queuedRef.current = null;
    markDone();
    setOpen(false);
  };

  const modalOpen = Boolean(step?.submitClick && queryTarget(step.submitClick));
  const waitingCollect = Boolean(needWait && submitted && !sessionDone);
  const liveTarget = modalOpen
    ? (step?.submitClick ?? null)
    : waitingCollect && queryTarget(PROGRESS_TARGET)
      ? PROGRESS_TARGET
      : (step?.target ?? null);
  const maskOff = modalOpen || waitingCollect;

  let description = step?.description;
  if (skipActions && (needClick || needSubmit || needWait)) {
    description = "当前是只读账号，无法执行采集操作，可直接下一步浏览菜单和页面。";
  } else if (needWait) {
    if (master.active) {
      description = "采集进行中，可看进度条或点终止。发布会等主站采完，完成后「下一步」才会亮起。";
    } else if (master.failed) {
      description = "这次采集失败或已停止，请重新走一遍「批量采集」。成功并发布后才能继续。";
    } else if (sessionDone) {
      description = "主站已采集完成（含发布），可以进入下一步看分类。";
    } else if (submitted) {
      description = "已发起采集。可继续看进度；主站采完并发布后，「下一步」才会亮起。";
    } else {
      description = "请先点「批量采集」，再在弹窗里点「开始采集」。";
    }
  }

  const currentStep: NonNullable<TourProps["steps"]>[number] = {
    title: step?.title,
    placement: step?.placement,
    target: () => queryTarget(liveTarget),
    description,
  };

  const last = current >= GUIDE_STEPS.length - 1;

  return (
    <Tour
      key={`${pathname}-${current}-${clicked}-${submitted}-${maskOff}-${targetTick}`}
      open={open}
      current={0}
      onClose={closeTour}
      steps={[currentStep]}
      mask={maskOff ? false : { color: "rgba(0,0,0,0.45)" }}
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
          {last ? "完成" : needWait && !canNext ? "等待采集完成" : "下一步"}
        </Button>,
      ]}
    />
  );
}

export function replayManageTour() {
  window.dispatchEvent(new Event(REPLAY_EVENT));
}
