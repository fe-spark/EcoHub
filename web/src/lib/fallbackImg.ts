/**
 * 无封面占位图 — 内联 SVG data URI，无需外部请求
 */
export const FALLBACK_IMG =
  "data:image/svg+xml," +
  encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="300" height="420" viewBox="0 0 300 420">
  <rect width="300" height="420" fill="#1a1a2e"/>
  <rect x="0" y="0" width="300" height="420" fill="url(#grad)"/>
  <defs>
    <linearGradient id="grad" x1="0%" y1="0%" x2="0%" y2="100%">
      <stop offset="0%" style="stop-color:#1f1f3a;stop-opacity:1"/>
      <stop offset="100%" style="stop-color:#0d0d1a;stop-opacity:1"/>
    </linearGradient>
  </defs>
  <!-- film icon -->
  <g transform="translate(150,185)" opacity="0.35">
    <rect x="-36" y="-28" width="72" height="56" rx="6" fill="none" stroke="#fff" stroke-width="3"/>
    <circle cx="-24" cy="-18" r="5" fill="#fff"/>
    <circle cx="-24" cy="18" r="5" fill="#fff"/>
    <circle cx="24" cy="-18" r="5" fill="#fff"/>
    <circle cx="24" cy="18" r="5" fill="#fff"/>
    <rect x="-14" y="-14" width="28" height="28" rx="3" fill="#fff" opacity="0.6"/>
    <rect x="-40" y="-20" width="6" height="12" rx="2" fill="#fff"/>
    <rect x="-40" y="8" width="6" height="12" rx="2" fill="#fff"/>
    <rect x="34" y="-20" width="6" height="12" rx="2" fill="#fff"/>
    <rect x="34" y="8" width="6" height="12" rx="2" fill="#fff"/>
  </g>
  <text x="150" y="265" text-anchor="middle" font-family="PingFang SC,Microsoft YaHei,sans-serif" font-size="14" fill="rgba(255,255,255,0.3)">暂无封面</text>
</svg>`);
