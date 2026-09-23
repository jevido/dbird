// Omarchy theme support: on Omarchy systems dbird takes its colors from the
// desktop theme (~/.local/state/omarchy/current/theme/colors.toml) and follows
// `omarchy theme set` live. Elsewhere the built-in dark theme is used.
import { Events } from '@wailsio/runtime';
import { ThemeService } from '../../bindings/dbird';
import type { OmarchyTheme } from '../../bindings/dbird/models';
import { monaco } from './monaco';

type RGB = [number, number, number];

const hex = (h: string): RGB => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16) || 0) as RGB;
const toHex = (c: RGB) => '#' + c.map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, '0')).join('');
// Mixes b into a by t (0 = a, 1 = b).
const mix = (a: string, b: string, t: number) => {
  const [x, y] = [hex(a), hex(b)];
  return toHex([0, 1, 2].map((i) => x[i]! + (y[i]! - x[i]!) * t) as RGB);
};
const alpha = (c: string, a: number) => c + Math.round(a * 255).toString(16).padStart(2, '0');
const saturation = (c: string) => {
  const [r, g, b] = hex(c).map((v) => v / 255) as RGB;
  const max = Math.max(r, g, b), min = Math.min(r, g, b);
  return max === 0 ? 0 : (max - min) / max;
};
// Some themes are near-monochrome in the base colors and only saturated in
// the bright ones (or the other way round); pick whichever reads as a color.
const vivid = (a: string | undefined, b: string | undefined, fallback: string) =>
  [a, b].filter((x): x is string => !!x).sort((x, y) => saturation(y) - saturation(x))[0] ?? fallback;

interface Palette {
  vars: Record<string, string>;
  monaco: monaco.editor.IStandaloneThemeData;
  scheme: 'dark' | 'light';
}

function palette(t: OmarchyTheme['theme']): Palette {
  const c: Record<string, string | undefined> = t.colors ?? {};
  const bg = c.background ?? '#1b1c20', fg = c.foreground ?? '#dcdee4';
  const accent = c.accent ?? c.blue ?? fg;
  const red = vivid(c.red, c.bright_red, '#f0716b');
  const green = vivid(c.green, c.bright_green, '#5cc38a');
  const selection = c.selection ?? mix(bg, accent, 0.3);
  const syn = {
    keyword: vivid(c.magenta, c.bright_magenta, accent),
    string: green,
    number: c.orange ?? vivid(c.yellow, c.bright_yellow, fg),
    comment: mix(fg, bg, 0.5),
    type: vivid(c.cyan, c.bright_cyan, accent),
    punct: mix(fg, bg, 0.25),
    ident: vivid(c.blue, c.bright_blue, accent),
  };
  const textMuted = mix(fg, bg, 0.35), textFaint = mix(fg, bg, 0.58);
  const panel2 = mix(bg, fg, 0.09), borderStrong = mix(bg, fg, 0.2);
  const vars: Record<string, string> = {
    '--bg': bg,
    '--sidebar-bg': c.dark_background ?? mix(bg, fg, 0.02),
    '--panel': mix(bg, fg, 0.05),
    '--panel-2': panel2,
    '--editor-bg': bg,
    '--grid-bg': bg,
    '--grid-odd': mix(bg, fg, 0.03),
    '--grid-line': mix(bg, fg, 0.09),
    '--grid-selrow': mix(bg, accent, 0.14),
    '--input-bg': c.darker_background ?? mix(bg, fg, 0.02),
    '--border': mix(bg, fg, 0.12),
    '--border-strong': borderStrong,
    '--hover': alpha(fg, 0.05),
    '--hover-strong': alpha(fg, 0.1),
    '--active-line': alpha(fg, 0.04),
    '--selection': selection,
    '--selection-match': mix(bg, selection, 0.5),
    '--flash': alpha(accent, 0.28),
    '--text': fg,
    '--text-muted': textMuted,
    '--text-faint': textFaint,
    '--accent': accent,
    '--accent-bg': alpha(accent, 0.16),
    '--success': green,
    '--success-bg': alpha(green, 0.12),
    '--danger': red,
    '--danger-bg': alpha(red, 0.12),
    '--syn-keyword': syn.keyword,
    '--syn-string': syn.string,
    '--syn-number': syn.number,
    '--syn-comment': syn.comment,
    '--syn-type': syn.type,
    '--syn-punct': syn.punct,
    '--syn-ident': syn.ident,
  };
  const m = (h: string) => h.slice(1);
  return {
    vars,
    scheme: t.mode === 'light' ? 'light' : 'dark',
    monaco: {
      base: t.mode === 'light' ? 'vs' : 'vs-dark',
      inherit: true,
      rules: [
        { token: '', foreground: m(fg) },
        { token: 'keyword', foreground: m(syn.keyword), fontStyle: 'bold' },
        { token: 'keyword.sql', foreground: m(syn.keyword), fontStyle: 'bold' },
        { token: 'operator', foreground: m(syn.punct) },
        { token: 'operator.sql', foreground: m(syn.punct) },
        { token: 'delimiter', foreground: m(syn.punct) },
        { token: 'delimiter.sql', foreground: m(syn.punct) },
        { token: 'string', foreground: m(syn.string) },
        { token: 'string.sql', foreground: m(syn.string) },
        { token: 'number', foreground: m(syn.number) },
        { token: 'number.sql', foreground: m(syn.number) },
        { token: 'comment', foreground: m(syn.comment), fontStyle: 'italic' },
        { token: 'comment.sql', foreground: m(syn.comment), fontStyle: 'italic' },
        { token: 'predefined', foreground: m(syn.type) },
        { token: 'predefined.sql', foreground: m(syn.type) },
        { token: 'identifier.quote', foreground: m(syn.ident) },
        { token: 'identifier.quote.sql', foreground: m(syn.ident) },
      ],
      colors: {
        'editor.background': bg,
        'editor.foreground': fg,
        'editorLineNumber.foreground': textFaint,
        'editorLineNumber.activeForeground': textMuted,
        'editorCursor.foreground': accent,
        'editor.selectionBackground': selection,
        'editor.inactiveSelectionBackground': mix(bg, selection, 0.6),
        'editor.lineHighlightBackground': alpha(fg, 0.04),
        'editor.lineHighlightBorder': '#00000000',
        'editorWidget.background': panel2,
        'editorWidget.border': borderStrong,
        'editorSuggestWidget.background': panel2,
        'editorSuggestWidget.border': borderStrong,
        'editorSuggestWidget.foreground': fg,
        'editorSuggestWidget.selectedBackground': alpha(accent, 0.25),
        'editorGutter.background': bg,
        'scrollbarSlider.background': alpha(fg, 0.15),
        'scrollbarSlider.hoverBackground': alpha(fg, 0.25),
        focusBorder: accent,
      },
    },
  };
}

let applied: string[] = [];

// The Monaco theme editors should use right now.
export let editorTheme = 'dbird-dark';

function apply(o: OmarchyTheme) {
  const root = document.documentElement;
  for (const k of applied) root.style.removeProperty(k);
  applied = [];
  if (!o.available) {
    root.style.colorScheme = 'dark';
    editorTheme = 'dbird-dark';
    monaco.editor.setTheme(editorTheme);
    return;
  }
  const p = palette(o.theme);
  for (const [k, v] of Object.entries(p.vars)) {
    root.style.setProperty(k, v);
    applied.push(k);
  }
  root.style.colorScheme = p.scheme;
  monaco.editor.defineTheme('dbird-omarchy', p.monaco);
  editorTheme = 'dbird-omarchy';
  monaco.editor.setTheme(editorTheme);
}

let off: (() => void) | undefined;

// Applies the Omarchy theme (if any) and follows changes to it.
export async function initTheme() {
  try {
    apply(await ThemeService.Omarchy());
  } catch {
    /* keep the built-in theme */
  }
  off?.();
  off = Events.On('theme:omarchy', (e: { data: OmarchyTheme }) => apply(e.data));
}
