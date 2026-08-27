const ENTER_MS = 260;
const LEAVE_MS = 150;

type Fx = {
  slot: HTMLElement;
  canvas: HTMLCanvasElement;
  small: HTMLCanvasElement;
  full: HTMLCanvasElement;
  started: number;
  leaving: boolean;
  leaveAt: number;
  raf: number;
};

function prefersReducedMotion() {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function lerp(a: number, b: number, t: number) {
  return a + (b - a) * t;
}

function coverSource(nw: number, nh: number, dw: number, dh: number) {
  const ir = nw / nh;
  const dr = dw / dh;
  if (ir > dr) {
    const sw = nh * dr;
    return { sx: (nw - sw) / 2, sy: 0, sw, sh: nh };
  }
  const sh = nw / dr;
  return { sx: 0, sy: (nh - sh) / 2, sw: nw, sh };
}

function tick(fx: Fx) {
  const img = fx.slot.querySelector("img");
  const box = img?.getBoundingClientRect();
  const w = Math.max(2, Math.round(box?.width ?? fx.slot.clientWidth));
  const h = Math.max(2, Math.round(box?.height ?? fx.slot.clientHeight));
  if (!img || !img.naturalWidth || w < 2 || h < 2) {
    fx.raf = requestAnimationFrame(() => tick(fx));
    return;
  }

  if (fx.canvas.width !== w || fx.canvas.height !== h) {
    fx.canvas.width = w;
    fx.canvas.height = h;
    fx.canvas.style.width = `${w}px`;
    fx.canvas.style.height = `${h}px`;
  }

  const now = performance.now();
  let block: number;
  let slice: number;
  let rgb: number;

  if (fx.leaving) {
    const t = Math.min(1, (now - fx.leaveAt) / LEAVE_MS);
    block = lerp(3.2, 1, t);
    slice = lerp(2.4, 0, t);
    rgb = lerp(2, 0, t);
    if (t >= 1) {
      stop(fx);
      return;
    }
  } else {
    const elapsed = now - fx.started;
    if (elapsed < ENTER_MS) {
      const t = elapsed / ENTER_MS;
      const e = 1 - (1 - t) * (1 - t);
      block = lerp(22, 2.6, e);
      slice = lerp(2, 0.8, e);
      rgb = lerp(2, 1, e);
    } else {
      const beat = elapsed % 220;
      const spike = beat < 36 ? 1 : 0;
      block = 2.2 + spike * 1.4;
      slice = 0.6 + spike * 0.8;
      rgb = 1 + spike * 0.6;
    }
  }

  const sw = Math.max(2, Math.round(w / Math.max(block, 1.5)));
  const sh = Math.max(2, Math.round((sw * h) / w));
  if (fx.small.width !== sw || fx.small.height !== sh) {
    fx.small.width = sw;
    fx.small.height = sh;
  }
  if (fx.full.width !== w || fx.full.height !== h) {
    fx.full.width = w;
    fx.full.height = h;
  }

  const sctx = fx.small.getContext("2d");
  const fctx = fx.full.getContext("2d");
  const ctx = fx.canvas.getContext("2d");
  if (!sctx || !fctx || !ctx) {
    return;
  }

  sctx.imageSmoothingEnabled = false;
  fctx.imageSmoothingEnabled = false;
  ctx.imageSmoothingEnabled = false;

  const src = coverSource(img.naturalWidth, img.naturalHeight, w, h);
  sctx.clearRect(0, 0, sw, sh);
  sctx.drawImage(img, src.sx, src.sy, src.sw, src.sh, 0, 0, sw, sh);
  fctx.clearRect(0, 0, w, h);
  fctx.drawImage(fx.small, 0, 0, sw, sh, 0, 0, w, h);

  ctx.clearRect(0, 0, w, h);
  const bands = 12;
  let y = 0;
  for (let i = 0; i < bands; i += 1) {
    const nextY = i === bands - 1 ? h : Math.round(((i + 1) * h) / bands);
    const bh = nextY - y;
    const ox = slice > 0.4 ? Math.round((Math.random() - 0.5) * 2 * slice) : 0;
    ctx.drawImage(fx.full, 0, y, w, bh, ox, y, w, bh);
    y = nextY;
  }

  if (rgb > 0.2) {
    ctx.globalCompositeOperation = "screen";
    ctx.globalAlpha = 0.22;
    ctx.drawImage(fx.full, rgb, 0);
    ctx.globalAlpha = 0.16;
    ctx.drawImage(fx.full, -rgb, 0);
    ctx.globalCompositeOperation = "source-over";
    ctx.globalAlpha = 1;
  }

  fx.raf = requestAnimationFrame(() => tick(fx));
}

function stop(fx: Fx) {
  cancelAnimationFrame(fx.raf);
  fx.raf = 0;
  const img = fx.slot.querySelector("img");
  if (img) {
    img.style.visibility = "";
  }
  fx.canvas.remove();
}

export function bindPixelHover(root: HTMLElement) {
  if (prefersReducedMotion()) {
    return () => undefined;
  }

  let fx: Fx | null = null;
  const small = document.createElement("canvas");
  const full = document.createElement("canvas");

  const begin = (slot: HTMLElement) => {
    if (fx?.slot === slot && !fx.leaving) {
      return;
    }
    if (fx) {
      stop(fx);
      fx = null;
    }
    const canvas = document.createElement("canvas");
    canvas.setAttribute("aria-hidden", "true");
    canvas.dataset.pixelFx = "1";
    canvas.style.cssText =
      "position:absolute;left:0;top:0;pointer-events:none;z-index:0;border-radius:inherit;display:block;";
    slot.appendChild(canvas);
    const cover = slot.querySelector("img");
    if (cover) {
      cover.style.visibility = "hidden";
    }
    fx = {
      slot,
      canvas,
      small,
      full,
      started: performance.now(),
      leaving: false,
      leaveAt: 0,
      raf: 0,
    };
    fx.raf = requestAnimationFrame(() => tick(fx as Fx));
  };

  const end = (slot: HTMLElement) => {
    if (!fx || fx.slot !== slot || fx.leaving) {
      return;
    }
    fx.leaving = true;
    fx.leaveAt = performance.now();
  };

  const onOver = (event: PointerEvent) => {
    const slot = (event.target as HTMLElement | null)?.closest?.("[data-pixel-hover]");
    if (!(slot instanceof HTMLElement) || !root.contains(slot)) {
      return;
    }
    begin(slot);
  };

  const onOut = (event: PointerEvent) => {
    const slot = (event.target as HTMLElement | null)?.closest?.("[data-pixel-hover]");
    if (!(slot instanceof HTMLElement)) {
      return;
    }
    const next = event.relatedTarget;
    if (next instanceof Node && slot.contains(next)) {
      return;
    }
    end(slot);
  };

  root.addEventListener("pointerover", onOver);
  root.addEventListener("pointerout", onOut);

  return () => {
    root.removeEventListener("pointerover", onOver);
    root.removeEventListener("pointerout", onOut);
    if (fx) {
      stop(fx);
      fx = null;
    }
  };
}
