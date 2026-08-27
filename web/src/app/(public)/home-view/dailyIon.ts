import { clampInCard, drawScraps, retargetSlotScraps, spawnScraps } from "./dailyIonScrap";

// 散开要飞到随机格点，比原先几像素抖动更远
const OUT_MS = 480;
const IN_MS = 500;
const MIN_HOLD_MS = 160;
const MAX_IMAGE_WAIT_MS = 1600;

export type IonPending = {
  apply: () => void;
};

function prefersReducedMotion() {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function easeOut(t: number) {
  return 1 - (1 - t) * (1 - t);
}

function easeInOut(t: number) {
  return t < 0.5 ? 2 * t * t : 1 - ((2 - 2 * t) * (2 - 2 * t)) / 2;
}

function animate(duration: number, onFrame: (t: number) => void, alive: () => boolean) {
  return new Promise<void>((resolve) => {
    const start = performance.now();
    const loop = (now: number) => {
      if (!alive()) {
        resolve();
        return;
      }
      const t = Math.min(1, (now - start) / duration);
      onFrame(t);
      if (t < 1) {
        requestAnimationFrame(loop);
      } else {
        resolve();
      }
    };
    requestAnimationFrame(loop);
  });
}

function holdUntil<T>(
  ctx: CanvasRenderingContext2D,
  scraps: ReturnType<typeof spawnScraps>,
  ready: Promise<T>,
  alive: () => boolean,
): Promise<T | null> {
  let done = false;
  ready.finally(() => {
    done = true;
  });
  return new Promise((resolve, reject) => {
    const start = performance.now();
    const loop = (now: number) => {
      if (!alive()) {
        resolve(null);
        return;
      }
      if (done) {
        ready.then(resolve, reject);
        return;
      }
      const sec = (now - start) / 1000;
      for (let i = 0; i < scraps.length; i += 1) {
        const p = scraps[i];
        const wave = Math.sin(sec * p.freq + p.phase);
        p.x = p.holdX + p.px * wave * p.amp;
        p.y = p.holdY + p.py * wave * p.amp * 0.5;
        p.w = p.homeW;
        p.h = p.homeH;
        clampInCard(p);
      }
      drawScraps(ctx, scraps, 1);
      requestAnimationFrame(loop);
    };
    requestAnimationFrame(loop);
  });
}

function fitCanvas(canvas: HTMLCanvasElement, stage: DOMRect) {
  const w = Math.max(1, Math.round(stage.width));
  const h = Math.max(1, Math.round(stage.height));
  if (canvas.width !== w) {
    canvas.width = w;
  }
  if (canvas.height !== h) {
    canvas.height = h;
  }
  canvas.style.width = "100%";
  canvas.style.height = "100%";
}

function slotSettled(slot: HTMLElement) {
  const img = slot.querySelector("img");
  if (!(img instanceof HTMLImageElement)) {
    return false;
  }
  return img.complete && img.naturalWidth > 0;
}

function clearIonVisuals(stage: HTMLElement, ctx: CanvasRenderingContext2D | null) {
  const marked = stage.querySelectorAll("[data-ion-fx]");
  for (let i = 0; i < marked.length; i += 1) {
    marked[i].removeAttribute("data-ion-fx");
  }
  if (ctx) {
    ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height);
  }
}

function markShatterSlots(slots: HTMLElement[]) {
  for (let i = 0; i < slots.length; i += 1) {
    const slot = slots[i];
    const img = slot.querySelector("img");
    if (img instanceof HTMLImageElement && img.naturalWidth > 0) {
      slot.setAttribute("data-ion-fx", "");
    } else {
      slot.removeAttribute("data-ion-fx");
    }
  }
}

function revealSettledSlots(stage: HTMLElement, force: boolean, onLeadReady?: () => void) {
  const slots = [...stage.querySelectorAll<HTMLElement>("[data-ion-slot]")];
  const hidden = new Set<number>();
  for (let i = 0; i < slots.length; i += 1) {
    const slot = slots[i];
    if (!slot.hasAttribute("data-ion-fx")) {
      continue;
    }
    if (force || slotSettled(slot)) {
      slot.removeAttribute("data-ion-fx");
      if (i === 0) {
        onLeadReady?.();
      }
    } else {
      hidden.add(i);
    }
  }
  return hidden;
}

async function applyPending(
  pending: Promise<IonPending | null>,
  onReveal: () => void,
  onLeadReady?: () => void,
) {
  const result = await pending.catch(() => null);
  result?.apply();
  onLeadReady?.();
  onReveal();
}

function isPageVisible() {
  return typeof document === "undefined" || !document.hidden;
}

export async function playDailyIonSwap(options: {
  canvas: HTMLCanvasElement;
  stage: HTMLElement;
  slots: HTMLElement[];
  pending: Promise<IonPending | null>;
  onHide: () => void;
  onReveal: () => void;
  onLeadReady?: () => void;
}) {
  const { canvas, stage, slots, pending, onHide, onReveal, onLeadReady } = options;
  const alive = () => canvas.isConnected && isPageVisible();
  let ctx: CanvasRenderingContext2D | null = null;
  if (prefersReducedMotion() || slots.length === 0 || !isPageVisible()) {
    await applyPending(pending, onReveal, onLeadReady);
    return;
  }

  const stageRect = stage.getBoundingClientRect();
  fitCanvas(canvas, stageRect);
  ctx = canvas.getContext("2d", { alpha: true });
  if (!ctx) {
    await applyPending(pending, onReveal, onLeadReady);
    return;
  }

  try {
    const scraps = spawnScraps(slots, stageRect);
    if (scraps.length === 0) {
      await applyPending(pending, onReveal, onLeadReady);
      return;
    }

    drawScraps(ctx, scraps, 1);
    markShatterSlots(slots);
    onHide();

    await animate(
      OUT_MS,
      (t) => {
        for (let i = 0; i < scraps.length; i += 1) {
          const p = scraps[i];
          const e = easeOut(Math.max(0, (t - p.delay) / Math.max(0.001, 1 - p.delay)));
          p.x = p.homeX + p.wx * e;
          p.y = p.homeY + p.wy * e;
          p.w = p.homeW;
          p.h = p.homeH;
          clampInCard(p);
        }
        drawScraps(ctx, scraps, 1);
      },
      alive,
    );

    for (let i = 0; i < scraps.length; i += 1) {
      scraps[i].holdX = scraps[i].x;
      scraps[i].holdY = scraps[i].y;
    }

    let holdTimer: number | undefined;
    const minHold = new Promise<void>((resolve) => {
      holdTimer = window.setTimeout(resolve, MIN_HOLD_MS);
    });
    const result = await holdUntil(
      ctx,
      scraps,
      Promise.all([pending.catch(() => null), minHold]).then(([next]) => next),
      alive,
    );
    if (holdTimer != null) {
      window.clearTimeout(holdTimer);
    }

    if (!alive()) {
      const fallbackResult = await pending.catch(() => null);
      fallbackResult?.apply();
      return;
    }

    // 将新数据应用到 React DOM 中挂载真实 <img>
    result?.apply();

    const nextRect = stage.getBoundingClientRect();
    fitCanvas(canvas, nextRect);
    const nextSlots = [...stage.querySelectorAll<HTMLElement>("[data-ion-slot]")];
    if (nextSlots.length !== slots.length) {
      revealSettledSlots(stage, true, onLeadReady);
      onReveal();
      return;
    }

    // 渐进式独立汇聚：先加载好的卡片先汇聚并展现，未加载好的卡片保持离子粒子浮动
    type SlotStatus = "holding" | "converging" | "done";
    interface SlotState {
      status: SlotStatus;
      startTime: number;
    }

    const slotStates: SlotState[] = nextSlots.map(() => ({
      status: "holding",
      startTime: 0,
    }));

    const holdStart = performance.now();
    const maxDeadline = holdStart + MAX_IMAGE_WAIT_MS;

    await new Promise<void>((resolve) => {
      const loop = (now: number) => {
        if (!alive()) {
          resolve();
          return;
        }

        let allDone = true;

        for (let s = 0; s < nextSlots.length; s += 1) {
          const state = slotStates[s];
          const slotEl = nextSlots[s];

          if (state.status === "holding") {
            allDone = false;
            // 该卡片图片已就绪，或达到最大等待超时，立即启动该卡片的汇聚动画
            if (slotSettled(slotEl) || now >= maxDeadline) {
              retargetSlotScraps(scraps, s, slotEl, nextRect, Boolean(result));
              state.status = "converging";
              state.startTime = now;
            } else {
              // 仍未就绪：继续自然离子态浮动
              const sec = (now - holdStart) / 1000;
              for (let i = 0; i < scraps.length; i += 1) {
                if (scraps[i].slotIndex === s) {
                  const p = scraps[i];
                  const wave = Math.sin(sec * p.freq + p.phase);
                  p.x = p.holdX + p.px * wave * p.amp;
                  p.y = p.holdY + p.py * wave * p.amp * 0.5;
                  clampInCard(p);
                }
              }
            }
          }

          if (state.status === "converging") {
            const elapsed = now - state.startTime;
            const t = Math.min(1, elapsed / IN_MS);

            for (let i = 0; i < scraps.length; i += 1) {
              if (scraps[i].slotIndex === s) {
                const p = scraps[i];
                const local = Math.max(0, (t - p.delay) / Math.max(0.001, 1 - p.delay));
                const e = easeInOut(local);
                p.x = p.holdX + (p.homeX - p.holdX) * e;
                p.y = p.holdY + (p.homeY - p.holdY) * e;
                p.w = p.homeW;
                p.h = p.homeH;
                p.mix = p.altImg ? Math.max(0, Math.min(1, (local - 0.12) / 0.28)) : 0;
                clampInCard(p);
              }
            }

            if (t >= 1) {
              state.status = "done";
              slotEl.removeAttribute("data-ion-fx");
              if (s === 0) {
                onLeadReady?.();
              }
            } else {
              allDone = false;
            }
          }
        }

        if (allDone) {
          resolve();
          return;
        }

        const activeScraps = scraps.filter((p) => slotStates[p.slotIndex]?.status !== "done");
        drawScraps(ctx, activeScraps, 1);
        requestAnimationFrame(loop);
      };

      requestAnimationFrame(loop);
    });

    if (!alive()) {
      return;
    }
    revealSettledSlots(stage, true, onLeadReady);
    onReveal();
  } finally {
    clearIonVisuals(stage, ctx);
    revealSettledSlots(stage, true, onLeadReady);
    onReveal();
  }
}
