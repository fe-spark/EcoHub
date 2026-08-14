const OUT_MS = 280;
const IN_MS = 480;
const MAX_PARTICLES = 720;
const PALETTE_DARK = ["#fa8c16", "#ffd27a", "#ffffff"];
const PALETTE_LIGHT = ["#9a3412", "#c2410c", "#ea580c", "#1e293b"];

function isLightTheme() {
  return typeof document !== "undefined" && document.documentElement.dataset.theme === "light";
}

function activePalette() {
  return isLightTheme() ? PALETTE_LIGHT : PALETTE_DARK;
}

type Particle = {
  x: number;
  y: number;
  ox: number;
  oy: number;
  tx: number;
  ty: number;
  dx: number;
  dy: number;
  size: number;
  color: string;
};

function prefersReducedMotion() {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function easeOut(t: number) {
  return 1 - (1 - t) * (1 - t);
}

function spawn(slots: HTMLElement[], stage: DOMRect, count: number): Particle[] {
  const palette = activePalette();
  const light = isLightTheme();
  const weights = slots.map((slot, index) => {
    const rect = slot.getBoundingClientRect();
    return index === 0 ? rect.width * rect.height * 1.8 : rect.width * rect.height;
  });
  const total = weights.reduce((sum, w) => sum + w, 0) || 1;
  const out: Particle[] = [];
  for (let s = 0; s < slots.length; s += 1) {
    const rect = slots[s].getBoundingClientRect();
    const n = Math.max(24, Math.round((weights[s] / total) * count));
    const ox = rect.left - stage.left;
    const oy = rect.top - stage.top;
    for (let i = 0; i < n && out.length < count; i += 1) {
      const px = ox + Math.random() * rect.width;
      const py = oy + Math.random() * rect.height;
      const ang = Math.random() * Math.PI * 2;
      const spd = 6 + Math.random() * 12;
      out.push({
        x: px,
        y: py,
        ox: px,
        oy: py,
        tx: px,
        ty: py,
        dx: Math.cos(ang) * spd,
        dy: Math.sin(ang) * spd,
        size: light ? 1.6 + Math.random() * 1.4 : 1 + Math.random() * 1.1,
        color: palette[i % palette.length],
      });
    }
  }
  return out;
}

function draw(ctx: CanvasRenderingContext2D, particles: Particle[], alpha: number) {
  const palette = activePalette();
  ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height);
  ctx.globalAlpha = isLightTheme() ? Math.min(1, alpha * 1.25) : alpha;
  for (let c = 0; c < palette.length; c += 1) {
    ctx.fillStyle = palette[c];
    for (let i = 0; i < particles.length; i += 1) {
      const p = particles[i];
      if (p.color !== palette[c]) {
        continue;
      }
      ctx.fillRect(p.x, p.y, p.size, p.size);
    }
  }
}

function animate(duration: number, onFrame: (t: number) => void) {
  return new Promise<void>((resolve) => {
    const start = performance.now();
    const loop = (now: number) => {
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

export async function preloadPictures(urls: string[]) {
  await Promise.all(
    urls.filter(Boolean).map(
      (url) =>
        new Promise<void>((resolve) => {
          const img = new Image();
          img.onload = () => resolve();
          img.onerror = () => resolve();
          img.src = url;
        }),
    ),
  );
}

export async function playDailyIonSwap(options: {
  canvas: HTMLCanvasElement;
  stage: HTMLElement;
  slots: HTMLElement[];
  nextPictures: string[];
  onHide: () => void;
  onSwap: () => void;
  onReveal: () => void;
}) {
  const { canvas, stage, slots, onHide, onSwap, onReveal } = options;
  if (prefersReducedMotion() || slots.length === 0) {
    onSwap();
    onReveal();
    return;
  }

  const stageRect = stage.getBoundingClientRect();
  canvas.width = Math.max(1, Math.round(stageRect.width));
  canvas.height = Math.max(1, Math.round(stageRect.height));
  canvas.style.width = "100%";
  canvas.style.height = "100%";
  const ctx = canvas.getContext("2d", { alpha: true });
  if (!ctx) {
    onSwap();
    onReveal();
    return;
  }

  const particles = spawn(slots, stageRect, MAX_PARTICLES);
  onHide();

  await animate(OUT_MS, (t) => {
    const e = easeOut(t);
    for (let i = 0; i < particles.length; i += 1) {
      const p = particles[i];
      p.x = p.ox + p.dx * e;
      p.y = p.oy + p.dy * e;
    }
    draw(ctx, particles, 1 - t * 0.35);
  });

  onSwap();
  await new Promise<void>((resolve) => {
    requestAnimationFrame(() => resolve());
  });

  const nextRect = stage.getBoundingClientRect();
  const nextSlots = [...stage.querySelectorAll<HTMLElement>("[data-ion-slot]")];
  const targets = spawn(nextSlots, nextRect, particles.length);
  for (let i = 0; i < particles.length; i += 1) {
    const p = particles[i];
    const dst = targets[i] || targets[0];
    p.ox = p.x;
    p.oy = p.y;
    p.tx = dst ? dst.tx : p.x;
    p.ty = dst ? dst.ty : p.y;
  }

  let revealed = false;
  await animate(IN_MS, (t) => {
    const e = easeOut(t);
    for (let i = 0; i < particles.length; i += 1) {
      const p = particles[i];
      p.x = p.ox + (p.tx - p.ox) * e;
      p.y = p.oy + (p.ty - p.oy) * e;
    }
    const fade = t < 0.5 ? 0.55 + t * 0.45 : 1 - (t - 0.5) * 2;
    draw(ctx, particles, Math.max(0, fade));
    if (!revealed && t >= 0.42) {
      revealed = true;
      onReveal();
    }
  });

  if (!revealed) {
    onReveal();
  }
  ctx.clearRect(0, 0, canvas.width, canvas.height);
}
