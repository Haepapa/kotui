/** Accent-colour theming utilities.
 *
 * All accent-related CSS custom properties are derived from a single
 * base hex colour and applied to :root.  The choice is persisted in
 * localStorage so it survives restarts.
 */

export const DEFAULT_ACCENT = '#d4a017';

const STORAGE_KEY = 'kotui-accent';

// ── colour math ─────────────────────────────────────────────────────────────

function hexToHsl(hex: string): [number, number, number] {
  const h = hex.replace('#', '');
  const r = parseInt(h.slice(0, 2), 16) / 255;
  const g = parseInt(h.slice(2, 4), 16) / 255;
  const b = parseInt(h.slice(4, 6), 16) / 255;
  const max = Math.max(r, g, b), min = Math.min(r, g, b);
  let hue = 0, sat = 0;
  const lit = (max + min) / 2;
  if (max !== min) {
    const d = max - min;
    sat = lit > 0.5 ? d / (2 - max - min) : d / (max + min);
    switch (max) {
      case r: hue = ((g - b) / d + (g < b ? 6 : 0)) / 6; break;
      case g: hue = ((b - r) / d + 2) / 6; break;
      case b: hue = ((r - g) / d + 4) / 6; break;
    }
  }
  return [hue * 360, sat * 100, lit * 100];
}

function hsl(h: number, s: number, l: number): string {
  return `hsl(${Math.round(h)},${Math.round(Math.max(0, Math.min(100, s)))}%,${Math.round(Math.max(0, Math.min(100, l)))}%)`;
}

function hsla(h: number, s: number, l: number, a: number): string {
  return `hsla(${Math.round(h)},${Math.round(s)}%,${Math.round(l)}%,${a})`;
}

// ── public API ───────────────────────────────────────────────────────────────

/** Apply a base accent hex to all accent CSS custom properties on :root. */
export function applyAccentColor(hex: string): void {
  const [h, s, l] = hexToHsl(hex);
  const root = document.documentElement;

  root.style.setProperty('--accent',           hex);
  // Buttons — sufficiently dark to be readable on dark backgrounds
  root.style.setProperty('--accent-btn',        hsl(h, Math.min(s, 82), l - 20));
  root.style.setProperty('--accent-btn-hover',  hsl(h, Math.min(s, 82), l - 14));
  // User chat bubble
  root.style.setProperty('--bubble-user-bg',    hsl(h, Math.min(s, 75), l - 26));
  root.style.setProperty('--bubble-user-text',  hsl(h, Math.min(s * 0.25, 25), Math.min(l + 44, 96)));
  // Translucent tints
  root.style.setProperty('--bg-active',              hsla(h, s, l, 0.15));
  root.style.setProperty('--bubble-tool-border',     hsla(h, s, l, 0.18));
  root.style.setProperty('--milestone-bg',           hsla(h, s, l, 0.10));
  root.style.setProperty('--milestone-border',       hsla(h, s, l, 0.25));
  // Lighter tones (text, icons)
  root.style.setProperty('--milestone-color',   hsl(h, s, Math.min(l + 12, 88)));
  root.style.setProperty('--active-hash',        hsl(h, Math.min(s, 90), Math.min(l + 16, 86)));
  root.style.setProperty('--nav-active-color',   hsl(h, Math.max(s - 12, 20), Math.min(l + 36, 93)));
  // Rail logo gradient stops
  root.style.setProperty('--logo-grad-start', hex);
  root.style.setProperty('--logo-grad-end',   hsl(h + 18, Math.min(s, 90), l - 6));
}

/** Load the saved accent colour (or the default) and apply it. */
export function loadAccentColor(): void {
  const saved = localStorage.getItem(STORAGE_KEY) ?? DEFAULT_ACCENT;
  applyAccentColor(saved);
}

/** Persist and apply a new accent colour. */
export function saveAccentColor(hex: string): void {
  localStorage.setItem(STORAGE_KEY, hex);
  applyAccentColor(hex);
}

/** Return the currently stored accent hex (or default). */
export function currentAccentColor(): string {
  return localStorage.getItem(STORAGE_KEY) ?? DEFAULT_ACCENT;
}
