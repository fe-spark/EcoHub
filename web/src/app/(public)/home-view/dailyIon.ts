import { clampInCard, drawScraps, retargetScraps, spawnScraps } from "./dailyIonScrap";

const OUT_MS = 380;
const IN_MS = 720;
const MIN_HOLD_MS = 240;

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

function nextFrame() {
  return new Promise<void>((resolve) => {
    requestAnimationFrame(() => resolve());
  });
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

async function waitSlotBitmaps(stage: HTMLElement, ms: number, alive: () => boolean) {
  const start = performance.now();
  while (alive() && performance.now() - start < ms) {
    const slots = [...stage.querySelectorAll<HTMLElement>("[data-ion-slot]")];
    const waiting = slots.some((slot) => slot.hasAttribute("data-ion-fx") && !slotSettled(slot));
    if (!waiting) {
      return;
    }
    await nextFrame();
  }
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
  const alive = () => canvas.isConnected;
  let ctx: CanvasRenderingContext2D | null = null;
  if (prefersReducedMotion() || slots.length === 0) {
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
    return;
  }

  result?.apply();
  await waitSlotBitmaps(stage, 1500, alive);
  if (!alive()) {
    return;
  }

  const nextRect = stage.getBoundingClientRect();
  fitCanvas(canvas, nextRect);
  const nextSlots = [...stage.querySelectorAll<HTMLElement>("[data-ion-slot]")];
  if (nextSlots.length !== slots.length) {
    revealSettledSlots(stage, true, onLeadReady);
    onReveal();
    return;
  }
  retargetScraps(scraps, nextSlots, nextRect, Boolean(result));
  drawScraps(ctx, scraps, 1);
  if (!nextSlots[0]?.hasAttribute("data-ion-fx")) {
    onLeadReady?.();
  }

  await animate(
    IN_MS,
    (t) => {
      for (let i = 0; i < scraps.length; i += 1) {
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
      drawScraps(ctx, scraps, 1);
    },
    alive,
  );

  if (!alive()) {
    return;
  }

  const until = performance.now() + 2000;
  while (alive()) {
    const hidden = revealSettledSlots(stage, performance.now() >= until, onLeadReady);
    const left = scraps.filter((p) => hidden.has(p.slotIndex));
    if (left.length === 0) {
      break;
    }
    const sec = performance.now() / 1000;
    for (let i = 0; i < left.length; i += 1) {
      const p = left[i];
      const wave = Math.sin(sec * p.freq + p.phase);
      p.x = p.homeX + p.px * wave * p.amp;
      p.y = p.homeY + p.py * wave * p.amp * 0.5;
      clampInCard(p);
    }
    drawScraps(ctx, left, 1);
    await nextFrame();
  }

  if (!alive()) {
    return;
  }
  revealSettledSlots(stage, true, onLeadReady);
  onReveal();
  } finally {
    clearIonVisuals(stage, ctx);
  }
}
