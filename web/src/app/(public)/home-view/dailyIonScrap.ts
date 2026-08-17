const CARD_RADIUS = 14;
const PIXEL = 8;
const PIXEL_COMPACT = 6;
const MAX_LEAD_TILES = 320;
const MAX_CARD_TILES = 200;
const PALETTE_DARK = ["#fa8c16", "#ffd27a", "#ffffff", "#f5e6c8"];
const PALETTE_LIGHT = ["#9a3412", "#c2410c", "#ea580c", "#1e293b"];

export type Scrap = {
  slotIndex: number;
  u: number;
  v: number;
  uw: number;
  uh: number;
  x: number;
  y: number;
  homeX: number;
  homeY: number;
  holdX: number;
  holdY: number;
  w: number;
  h: number;
  homeW: number;
  homeH: number;
  boxL: number;
  boxT: number;
  boxR: number;
  boxB: number;
  wx: number;
  wy: number;
  px: number;
  py: number;
  delay: number;
  phase: number;
  freq: number;
  amp: number;
  color: string;
  img: CanvasImageSource | null;
  sx: number;
  sy: number;
  sw: number;
  sh: number;
  altImg: CanvasImageSource | null;
  asx: number;
  asy: number;
  asw: number;
  ash: number;
  mix: number;
};

type Piece = { x: number; y: number; w: number; h: number };

function isLightTheme() {
  return typeof document !== "undefined" && document.documentElement.dataset.theme === "light";
}

function activePalette() {
  return isLightTheme() ? PALETTE_LIGHT : PALETTE_DARK;
}

function scrapAlpha(alpha: number) {
  return isLightTheme() ? Math.min(1, alpha * 1.12) : alpha;
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

/** 均匀像素格，粒度接近悬停；按卡封顶避免上千次 drawImage */
function splitPixels(width: number, height: number, compact: boolean, lead: boolean): Piece[] {
  const maxTiles = lead ? MAX_LEAD_TILES : MAX_CARD_TILES;
  let cell = compact ? PIXEL_COMPACT : PIXEL;
  let cols = Math.max(2, Math.round(width / cell));
  let rows = Math.max(2, Math.round(height / cell));
  while (cols * rows > maxTiles && cell < 48) {
    cell += 1;
    cols = Math.max(2, Math.round(width / cell));
    rows = Math.max(2, Math.round(height / cell));
  }
  const cw = width / cols;
  const ch = height / rows;
  const out: Piece[] = [];
  for (let row = 0; row < rows; row += 1) {
    for (let col = 0; col < cols; col += 1) {
      out.push({ x: col * cw, y: row * ch, w: cw, h: ch });
    }
  }
  return out;
}

function slotImage(slot: HTMLElement): HTMLImageElement | null {
  const img = slot.querySelector("img");
  return img instanceof HTMLImageElement && img.naturalWidth > 0 ? img : null;
}

/** 先缩小再放大，和悬停一样做成像素块 */
function pixelate(img: HTMLImageElement, dw: number, dh: number, block: number) {
  if (img.naturalWidth < 2 || img.naturalHeight < 2 || dw < 2 || dh < 2) {
    return null;
  }
  const sw = Math.max(2, Math.round(dw / Math.max(block, 1.5)));
  const sh = Math.max(2, Math.round((sw * dh) / dw));
  const small = document.createElement("canvas");
  small.width = sw;
  small.height = sh;
  const full = document.createElement("canvas");
  full.width = Math.max(1, Math.round(dw));
  full.height = Math.max(1, Math.round(dh));
  const sctx = small.getContext("2d");
  const fctx = full.getContext("2d");
  if (!sctx || !fctx) {
    return null;
  }
  sctx.imageSmoothingEnabled = false;
  fctx.imageSmoothingEnabled = false;
  const src = coverSource(img.naturalWidth, img.naturalHeight, dw, dh);
  sctx.drawImage(img, src.sx, src.sy, src.sw, src.sh, 0, 0, sw, sh);
  fctx.drawImage(small, 0, 0, sw, sh, 0, 0, full.width, full.height);
  return full;
}

function bindFromPixel(
  p: Scrap,
  pix: HTMLCanvasElement | null,
  fallback: CanvasImageSource | null,
  dest: "img" | "alt",
) {
  const src = pix ?? fallback;
  const nw = src instanceof HTMLCanvasElement ? src.width : src instanceof HTMLImageElement ? src.naturalWidth : 0;
  const nh = src instanceof HTMLCanvasElement ? src.height : src instanceof HTMLImageElement ? src.naturalHeight : 0;
  if (!src || nw < 2 || nh < 2) {
    if (dest === "img") {
      p.img = null;
      p.sx = 0;
      p.sy = 0;
      p.sw = 0;
      p.sh = 0;
    } else {
      p.altImg = null;
      p.asx = 0;
      p.asy = 0;
      p.asw = 0;
      p.ash = 0;
    }
    return;
  }
  const sx = p.u * nw;
  const sy = p.v * nh;
  const sw = p.uw * nw;
  const sh = p.uh * nh;
  if (dest === "img") {
    p.img = src;
    p.sx = sx;
    p.sy = sy;
    p.sw = sw;
    p.sh = sh;
    return;
  }
  p.altImg = src;
  p.asx = sx;
  p.asy = sy;
  p.asw = sw;
  p.ash = sh;
}

function shuffleOrder(n: number) {
  const order = Array.from({ length: n }, (_, i) => i);
  for (let i = n - 1; i > 0; i -= 1) {
    const j = Math.floor(Math.random() * (i + 1));
    const tmp = order[i];
    order[i] = order[j];
    order[j] = tmp;
  }
  if (n > 1 && order.every((v, i) => v === i)) {
    order[0] = 1;
    order[1] = 0;
  }
  return order;
}

function layout(p: Scrap, slot: DOMRect, stage: DOMRect) {
  p.w = p.uw * slot.width;
  p.h = p.uh * slot.height;
  p.homeW = p.w;
  p.homeH = p.h;
  p.boxL = slot.left - stage.left;
  p.boxT = slot.top - stage.top;
  p.boxR = p.boxL + slot.width;
  p.boxB = p.boxT + slot.height;
  p.homeX = p.boxL + (p.u + p.uw / 2) * slot.width;
  p.homeY = p.boxT + (p.v + p.uh / 2) * slot.height;
  p.x = p.homeX;
  p.y = p.homeY;
}

export function clampInCard(p: Scrap) {
  const hx = Math.max(1, p.w * 0.5);
  const hy = Math.max(1, p.h * 0.5);
  const minX = p.boxL + hx;
  const maxX = p.boxR - hx;
  const minY = p.boxT + hy;
  const maxY = p.boxB - hy;
  p.x = maxX < minX ? (p.boxL + p.boxR) / 2 : Math.min(maxX, Math.max(minX, p.x));
  p.y = maxY < minY ? (p.boxT + p.boxB) / 2 : Math.min(maxY, Math.max(minY, p.y));
}

export function spawnScraps(slots: HTMLElement[], stage: DOMRect): Scrap[] {
  const palette = activePalette();
  const compact = stage.width < 720;
  const out: Scrap[] = [];
  for (let s = 0; s < slots.length; s += 1) {
    const rect = slots[s].getBoundingClientRect();
    if (rect.width < 8 || rect.height < 8) {
      continue;
    }
    const img = slotImage(slots[s]);
    if (!img) {
      continue;
    }
    const block = compact ? PIXEL_COMPACT : PIXEL;
    const pix = pixelate(img, rect.width, rect.height, block);
    const pieces = splitPixels(rect.width, rect.height, compact, s === 0);
    for (let i = 0; i < pieces.length; i += 1) {
      const piece = pieces[i];
      const ang = Math.random() * Math.PI * 2;
      const force = 2 + Math.random() * 4;
      const p: Scrap = {
        slotIndex: s,
        u: piece.x / rect.width,
        v: piece.y / rect.height,
        uw: piece.w / rect.width,
        uh: piece.h / rect.height,
        x: 0,
        y: 0,
        homeX: 0,
        homeY: 0,
        holdX: 0,
        holdY: 0,
        w: 0,
        h: 0,
        homeW: 0,
        homeH: 0,
        boxL: 0,
        boxT: 0,
        boxR: 0,
        boxB: 0,
        wx: Math.cos(ang) * force,
        wy: Math.sin(ang) * force,
        px: -Math.sin(ang),
        py: Math.cos(ang),
        delay: Math.random() * 0.08,
        phase: Math.random() * Math.PI * 2,
        freq: 1 + Math.random() * 0.8,
        amp: 2 + Math.random() * 4,
        color: palette[(s + i) % palette.length],
        img: null,
        sx: 0,
        sy: 0,
        sw: 0,
        sh: 0,
        altImg: null,
        asx: 0,
        asy: 0,
        asw: 0,
        ash: 0,
        mix: 0,
      };
      layout(p, rect, stage);
      bindFromPixel(p, pix, img, "img");
      out.push(p);
    }
  }
  return out;
}

export function retargetScraps(
  scraps: Scrap[],
  slots: HTMLElement[],
  stage: DOMRect,
  shuffle: boolean,
) {
  const groups = new Map<number, number[]>();
  for (let i = 0; i < scraps.length; i += 1) {
    const list = groups.get(scraps[i].slotIndex) ?? [];
    list.push(i);
    groups.set(scraps[i].slotIndex, list);
  }

  groups.forEach((idxs) => {
    const cells = idxs.map((i) => {
      const p = scraps[i];
      return { u: p.u, v: p.v, uw: p.uw, uh: p.uh };
    });
    const order = shuffle ? shuffleOrder(idxs.length) : idxs.map((_, i) => i);
    const slot = slots[scraps[idxs[0]].slotIndex];
    if (!slot) {
      return;
    }
    const rect = slot.getBoundingClientRect();
    const compact = stage.width < 720;
    const block = compact ? PIXEL_COMPACT : PIXEL;
    const nextImg = slotImage(slot);
    const pix = nextImg ? pixelate(nextImg, rect.width, rect.height, block) : null;
    for (let k = 0; k < idxs.length; k += 1) {
      const p = scraps[idxs[k]];
      const cell = cells[order[k]];
      const hx = p.x;
      const hy = p.y;
      if (cell) {
        p.u = cell.u;
        p.v = cell.v;
        p.uw = cell.uw;
        p.uh = cell.uh;
        p.boxL = rect.left - stage.left;
        p.boxT = rect.top - stage.top;
        p.boxR = p.boxL + rect.width;
        p.boxB = p.boxT + rect.height;
        p.homeW = p.uw * rect.width;
        p.homeH = p.uh * rect.height;
        p.w = p.homeW;
        p.h = p.homeH;
        p.homeX = p.boxL + (p.u + p.uw / 2) * rect.width;
        p.homeY = p.boxT + (p.v + p.uh / 2) * rect.height;
        if (nextImg) {
          bindFromPixel(p, pix, nextImg, "alt");
        }
      }
      p.x = hx;
      p.y = hy;
      p.mix = 0;
      p.delay = Math.random() * 0.18;
      clampInCard(p);
      p.holdX = p.x;
      p.holdY = p.y;
    }
  });
}

function paintTile(ctx: CanvasRenderingContext2D, p: Scrap, alpha: number) {
  const mix = Math.max(0, Math.min(1, p.mix));
  const oldOk = Boolean(p.img && p.sw > 1 && p.sh > 1);
  const nextOk = Boolean(p.altImg && p.asw > 1 && p.ash > 1);
  const dx = p.x - p.w / 2;
  const dy = p.y - p.h / 2;
  if (oldOk && nextOk && mix > 0 && mix < 1) {
    ctx.globalAlpha = alpha * (1 - mix);
    ctx.drawImage(p.img as CanvasImageSource, p.sx, p.sy, p.sw, p.sh, dx, dy, p.w, p.h);
    ctx.globalAlpha = alpha * mix;
    ctx.drawImage(p.altImg as CanvasImageSource, p.asx, p.asy, p.asw, p.ash, dx, dy, p.w, p.h);
    return;
  }
  ctx.globalAlpha = alpha;
  if (nextOk && mix >= 1) {
    ctx.drawImage(p.altImg as CanvasImageSource, p.asx, p.asy, p.asw, p.ash, dx, dy, p.w, p.h);
    return;
  }
  if (oldOk) {
    ctx.drawImage(p.img as CanvasImageSource, p.sx, p.sy, p.sw, p.sh, dx, dy, p.w, p.h);
    return;
  }
  ctx.fillStyle = p.color;
  ctx.fillRect(dx, dy, p.w, p.h);
}

export function drawScraps(ctx: CanvasRenderingContext2D, scraps: Scrap[], alpha: number) {
  ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height);
  ctx.imageSmoothingEnabled = false;
  const a = scrapAlpha(alpha);
  let i = 0;
  while (i < scraps.length) {
    const slot = scraps[i].slotIndex;
    const start = i;
    while (i < scraps.length && scraps[i].slotIndex === slot) {
      i += 1;
    }
    const box = scraps[start];
    ctx.save();
    ctx.beginPath();
    const bw = box.boxR - box.boxL;
    const bh = box.boxB - box.boxT;
    if (typeof ctx.roundRect === "function") {
      ctx.roundRect(box.boxL, box.boxT, bw, bh, CARD_RADIUS);
    } else {
      ctx.rect(box.boxL, box.boxT, bw, bh);
    }
    ctx.clip();
    for (let k = start; k < i; k += 1) {
      paintTile(ctx, scraps[k], a);
    }
    ctx.restore();
  }
  ctx.globalAlpha = 1;
}
