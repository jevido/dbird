<script lang="ts">
  import type { Result } from './state.svelte';
  import { Clipboard } from '@wailsio/runtime';
  import { app } from './state.svelte';
  import { editsFor } from './gridedits.svelte';

  let { result }: { result: Result } = $props();

  const edits = $derived(editsFor(result));
  // Result column c maps to a table column that can be edited.
  const canEdit = (c: number) => !!result.editable?.columns?.[c];
  // The cell being edited: r is the row index in result.rows.
  let editing = $state<{ r: number; c: number; text: string; wasNull: boolean } | null>(null);
  let menu = $state<{ x: number; y: number; r: number; c: number } | null>(null);

  const ROW_H = 24;
  const OVERSCAN = 12;

  let scrollTop = $state(0);
  let viewportH = $state(400);
  let selected = $state<{ r: number; c: number } | null>(null);
  let sort = $state<{ col: number; dir: 1 | -1 } | null>(null);
  let widths = $state<number[]>([]);
  let viewport: HTMLDivElement | undefined = $state();
  let viewing = $state<{ column: string; type: string; value: string | null } | null>(null);
  let pretty = $state(true);

  const viewText = $derived.by(() => {
    const v = viewing?.value;
    if (v == null) return 'NULL';
    if (pretty && /^\s*[[{]/.test(v)) {
      try {
        return JSON.stringify(JSON.parse(v), null, 2);
      } catch {
        /* not JSON */
      }
    }
    return v;
  });

  const columns = $derived(result.columns ?? []);
  const rawRows = $derived((result.rows ?? []) as (string | null)[][]);

  // Reset view state when a new result arrives.
  $effect.pre(() => {
    void result;
    selected = null;
    editing = null;
    menu = null;
    sort = null;
    scrollTop = 0;
    if (viewport) viewport.scrollTop = 0;
  });

  // Initial column widths from header and a sample of the data.
  $effect.pre(() => {
    const sample = rawRows.slice(0, 200);
    widths = columns.map((c, i) => {
      // Header shows name and type side by side.
      let len = c.name.length + (c.type?.length ?? 0) * 0.85 + 3;
      for (const row of sample) {
        const v = row[i];
        len = Math.max(len, v == null ? 4 : Math.min(v.length, 60));
      }
      return Math.round(Math.min(420, Math.max(64, len * 7.3 + 20)));
    });
  });

  function compare(a: string | null, b: string | null): number {
    if (a === b) return 0;
    if (a == null) return 1;
    if (b == null) return -1;
    const na = Number(a);
    const nb = Number(b);
    if (a.trim() !== '' && b.trim() !== '' && !Number.isNaN(na) && !Number.isNaN(nb)) return na - nb;
    return a.localeCompare(b);
  }

  // Row indices (into result.rows) in display order. Sorting uses the values
  // as queried, so rows don't jump around while being edited.
  const order = $derived.by(() => {
    void edits.version;
    const idx = rawRows.map((_, i) => i);
    if (!sort) return idx;
    const { col, dir } = sort;
    return idx.sort((a, b) => dir * compare(rawRows[a][col], rawRows[b][col]));
  });
  const cell = (r: number, c: number) => (void edits.version, edits.value(r, c));
  const rowValues = (r: number) => columns.map((_, c) => cell(r, c));

  const first = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN));
  const last = $derived(Math.min(order.length, Math.ceil((scrollTop + viewportH) / ROW_H) + OVERSCAN));
  const visible = $derived(order.slice(first, last));
  const template = $derived(`52px ${widths.map((w) => w + 'px').join(' ')}`);
  const totalW = $derived(52 + widths.reduce((a, b) => a + b, 0));

  function toggleSort(i: number) {
    if (!sort || sort.col !== i) sort = { col: i, dir: 1 };
    else if (sort.dir === 1) sort = { col: i, dir: -1 };
    else sort = null;
  }

  function startResize(e: PointerEvent, i: number) {
    e.preventDefault();
    e.stopPropagation();
    const startX = e.clientX;
    const startW = widths[i];
    const move = (ev: PointerEvent) => (widths[i] = Math.max(40, startW + ev.clientX - startX));
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  }

  function tsvCell(v: string | null): string {
    if (v == null) return '';
    return /[\t\n"]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v;
  }

  async function copy(text: string, what: string) {
    try {
      await Clipboard.SetText(text);
      app.toast(`Copied ${what}`);
    } catch {
      try {
        await navigator.clipboard.writeText(text);
        app.toast(`Copied ${what}`);
      } catch (e) {
        app.toast('Copy failed', 'error');
      }
    }
  }

  export function copyAll() {
    const head = columns.map((c) => tsvCell(c.name)).join('\t');
    const body = order.map((r) => rowValues(r).map(tsvCell).join('\t')).join('\n');
    copy(head + '\n' + body, `${order.length} rows`);
  }

  function view(r: number, c: number) {
    viewing = { column: columns[c].name, type: columns[c].type, value: cell(r, c) };
  }

  // Starts editing a cell, with text replacing its value when given (typing
  // on a selected cell).
  function startEdit(r: number, c: number, text?: string) {
    if (!canEdit(c)) {
      app.toast(`Read-only: ${result.editable ? 'this column can\'t be edited here' : result.readOnly || 'not editable'}`);
      return;
    }
    const v = cell(r, c);
    editing = { r, c, text: text ?? v ?? '', wasNull: v == null && text == null };
  }

  function commitEdit() {
    if (!editing) return;
    const { r, c, text, wasNull } = editing;
    editing = null;
    // An untouched NULL stays NULL rather than becoming ''.
    if (!(wasNull && text === '')) edits.set(r, c, text);
    viewport?.focus();
  }

  function cancelEdit() {
    editing = null;
    viewport?.focus();
  }

  function onEditKey(e: KeyboardEvent) {
    e.stopPropagation();
    if (e.key === 'Enter' && !e.shiftKey && !e.altKey) {
      e.preventDefault();
      commitEdit();
      moveSelection(1, 0);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      cancelEdit();
    } else if (e.key === 'Tab') {
      e.preventDefault();
      commitEdit();
      moveSelection(0, e.shiftKey ? -1 : 1);
    } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
      // Let the results panel save, including this cell.
      commitEdit();
      viewport?.dispatchEvent(new KeyboardEvent('keydown', { key: 's', ctrlKey: true, bubbles: true }));
      e.preventDefault();
    }
  }

  function moveSelection(dr: number, dc: number) {
    if (!selected) return;
    const nr = Math.max(0, Math.min(order.length - 1, selected.r + dr));
    const nc = Math.max(0, Math.min(columns.length - 1, selected.c + dc));
    selected = { r: nr, c: nc };
    if (viewport) {
      const top = nr * ROW_H;
      if (top < viewport.scrollTop) viewport.scrollTop = top;
      else if (top + ROW_H * 2 > viewport.scrollTop + viewportH) viewport.scrollTop = top + ROW_H * 2 - viewportH;
    }
  }

  // Closes the context menu and runs fn on the cell it was opened on.
  function menuAction(fn: (r: number, c: number) => void) {
    if (!menu) return;
    const { r, c } = menu;
    menu = null;
    fn(r, c);
  }

  function openMenu(e: MouseEvent, pos: number, c: number) {
    e.preventDefault();
    selected = { r: pos, c };
    menu = { x: e.clientX, y: e.clientY, r: order[pos], c };
  }

  function onkeydown(e: KeyboardEvent) {
    if (!selected) return;
    const { r: pos, c } = selected;
    const r = order[pos];
    const move = (dr: number, dc: number) => {
      moveSelection(dr, dc);
      e.preventDefault();
    };
    switch (e.key) {
      case 'ArrowDown': return move(1, 0);
      case 'ArrowUp': return move(-1, 0);
      case 'ArrowLeft': return move(0, -1);
      case 'ArrowRight': return move(0, 1);
      case 'PageDown': return move(Math.floor(viewportH / ROW_H), 0);
      case 'PageUp': return move(-Math.floor(viewportH / ROW_H), 0);
      case 'Home': return move(-pos, 0);
      case 'End': return move(order.length, 0);
    }
    const mod = e.ctrlKey || e.metaKey;
    if (e.key === 'F2' || (e.key === 'Enter' && !e.shiftKey && result.editable)) {
      e.preventDefault();
      startEdit(r, c);
      return;
    }
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      view(r, c);
      return;
    }
    if (mod && e.key === 'c') {
      e.preventDefault();
      if (e.shiftKey) copy(rowValues(r).map(tsvCell).join('\t'), 'row');
      else copy(cell(r, c) ?? '', 'value');
      return;
    }
    // Typing on an editable cell starts editing it, like a spreadsheet.
    if (!mod && !e.altKey && e.key.length === 1 && canEdit(c)) {
      e.preventDefault();
      startEdit(r, c, e.key);
    }
  }
</script>

{#if columns.length === 0}
  <div class="empty">Statement returned no columns.</div>
{:else}
  <div
    class="viewport"
    bind:this={viewport}
    bind:clientHeight={viewportH}
    onscroll={(e) => (scrollTop = e.currentTarget.scrollTop)}
    tabindex="0"
    role="grid"
    aria-rowcount={order.length}
    {onkeydown}
  >
    <div class="head" style:grid-template-columns={template} style:width="{totalW}px">
      <div class="cell rownum corner">#</div>
      {#each columns as col, i (i)}
        <button class="cell hcell" title="{col.name} ({col.type})" onclick={() => toggleSort(i)}>
          <span class="hname">{col.name}</span>
          <span class="htype">{col.type}</span>
          {#if sort?.col === i}<span class="sort">{sort.dir === 1 ? '▲' : '▼'}</span>{/if}
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
          <span
            class="resize"
            role="separator"
            aria-orientation="vertical"
            onpointerdown={(e) => startResize(e, i)}
            onclick={(e) => e.stopPropagation()}
          ></span>
        </button>
      {/each}
    </div>
    <div class="body" style:height="{order.length * ROW_H}px" style:width="{totalW}px">
      {#each visible as r, k (r)}
        {@const pos = first + k}
        <div
          class={['row', pos % 2 === 1 && 'odd', selected?.r === pos && 'selrow', edits.cells[r] && 'dirty']}
          style:grid-template-columns={template}
          style:transform="translateY({pos * ROW_H}px)"
        >
          <div class="cell rownum">{pos + 1}</div>
          {#each columns as _, c (c)}
            {@const v = cell(r, c)}
            {#if editing?.r === r && editing.c === c}
              <div class="cell editing">
                <!-- svelte-ignore a11y_autofocus -->
                <textarea
                  bind:value={editing.text}
                  placeholder={editing.wasNull ? 'NULL' : ''}
                  rows="1"
                  spellcheck="false"
                  onkeydown={onEditKey}
                  onblur={commitEdit}
                  {@attach (el) => {
                    el.focus();
                    el.setSelectionRange(el.value.length, el.value.length);
                  }}
                ></textarea>
              </div>
            {:else}
              <div
                class={[
                  'cell',
                  v == null && 'null',
                  edits.edited(r, c) && 'changed',
                  selected?.r === pos && selected?.c === c && 'sel',
                ]}
                role="gridcell"
                tabindex="-1"
                title={v ?? 'NULL'}
                onmousedown={() => (selected = { r: pos, c })}
                ondblclick={() => (result.editable && canEdit(c) ? startEdit(r, c) : view(r, c))}
                oncontextmenu={(e) => openMenu(e, pos, c)}
              >
                {v == null ? 'NULL' : v.length > 300 ? v.slice(0, 300) + '…' : v}
              </div>
            {/if}
          {/each}
        </div>
      {/each}
    </div>
  </div>
{/if}

{#if menu}
  {@const m = menu}
  <div class="mbackdrop" role="presentation" onmousedown={() => (menu = null)} oncontextmenu={(e) => (e.preventDefault(), (menu = null))}></div>
  <div class="cmenu" style:left="{m.x}px" style:top="{m.y}px" role="menu">
    {#if canEdit(m.c)}
      <button role="menuitem" onclick={() => menuAction((r, c) => startEdit(r, c))}>Edit <kbd>F2</kbd></button>
      <button role="menuitem" onclick={() => menuAction((r, c) => edits.set(r, c, null))} disabled={cell(m.r, m.c) == null}>Set to NULL</button>
      <button role="menuitem" onclick={() => menuAction((r, c) => edits.revert(r, c))} disabled={!edits.edited(m.r, m.c)}>Revert value</button>
      <div class="msep"></div>
    {/if}
    <button role="menuitem" onclick={() => menuAction((r, c) => view(r, c))}>View value</button>
    <button role="menuitem" onclick={() => menuAction((r, c) => copy(cell(r, c) ?? '', 'value'))}>Copy value</button>
    <button role="menuitem" onclick={() => menuAction((r) => copy(rowValues(r).map(tsvCell).join('\t'), 'row'))}>Copy row</button>
  </div>
{/if}

{#if viewing}
  <div class="vbackdrop" role="presentation" onmousedown={(e) => e.target === e.currentTarget && (viewing = null)}>
    <div
      class="viewer"
      role="dialog"
      aria-label="Value of {viewing.column}"
      tabindex="-1"
      onkeydown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation();
          viewing = null;
          viewport?.focus();
        }
      }}
      {@attach (el) => el.focus()}
    >
      <header>
        <strong>{viewing.column}</strong>
        <span class="vtype">{viewing.type}</span>
        <span class="vlen">{viewing.value == null ? 'NULL' : `${viewing.value.length.toLocaleString()} chars`}</span>
        <span class="vspacer"></span>
        <label class="vpretty"><input type="checkbox" bind:checked={pretty} /> Format JSON</label>
        <button class="btn sm" onclick={() => copy(viewing?.value ?? '', 'value')}>Copy</button>
        <button class="btn sm" onclick={() => (viewing = null)}>Close</button>
      </header>
      <pre class={[viewing.value == null && 'null']}>{viewText}</pre>
    </div>
  </div>
{/if}

<style>
  .vbackdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
    background: rgba(0, 0, 0, 0.45);
    display: grid;
    place-items: center;
    padding: 24px;
  }
  .viewer {
    width: min(900px, 100%);
    max-height: 80vh;
    display: flex;
    flex-direction: column;
    background: var(--panel);
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
    outline: none;
    overflow: hidden;
  }
  .viewer header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
    font-size: 12.5px;
    flex-wrap: wrap;
  }
  .vtype,
  .vlen {
    color: var(--text-faint);
    font-family: var(--mono);
    font-size: 11.5px;
  }
  .vspacer {
    flex: 1;
  }
  .vpretty {
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--text-muted);
    font-size: 12px;
  }
  .viewer pre {
    margin: 0;
    padding: 12px 14px;
    overflow: auto;
    font-family: var(--mono);
    font-size: 12.5px;
    white-space: pre-wrap;
    word-break: break-word;
    user-select: text;
  }
  .viewer pre.null {
    color: var(--text-faint);
    font-style: italic;
  }
  .viewport {
    position: relative;
    overflow: auto;
    height: 100%;
    font-family: var(--mono);
    font-size: 12px;
    outline: none;
    background: var(--grid-bg);
  }
  .head {
    position: sticky;
    top: 0;
    z-index: 2;
    display: grid;
    background: var(--panel);
    border-bottom: 1px solid var(--border-strong);
  }
  .body {
    position: relative;
  }
  .row {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    display: grid;
    height: 24px;
  }
  .row.odd {
    background: var(--grid-odd);
  }
  .row.selrow {
    background: var(--grid-selrow);
  }
  .cell {
    padding: 0 8px;
    line-height: 24px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    border-right: 1px solid var(--grid-line);
    color: var(--text);
    min-width: 0;
  }
  .cell.null {
    color: var(--text-faint);
    font-style: italic;
  }
  .row.dirty .rownum {
    color: var(--syn-number);
    box-shadow: inset 3px 0 var(--syn-number);
  }
  .cell.changed {
    background: color-mix(in srgb, var(--syn-number) 18%, transparent);
    color: var(--text);
    font-style: normal;
  }
  .cell.editing {
    padding: 0;
    overflow: visible;
    position: relative;
    z-index: 4;
  }
  .cell.editing textarea {
    display: block;
    width: 100%;
    min-height: 24px;
    height: 24px;
    margin: 0;
    padding: 0 7px;
    border: 2px solid var(--accent);
    border-radius: 0;
    background: var(--input-bg);
    color: var(--text);
    font: inherit;
    line-height: 20px;
    resize: none;
    outline: none;
    overflow: hidden;
    white-space: pre;
  }
  .mbackdrop {
    position: fixed;
    inset: 0;
    z-index: 55;
  }
  .cmenu {
    position: fixed;
    z-index: 56;
    min-width: 170px;
    padding: 4px;
    background: var(--panel);
    border: 1px solid var(--border-strong);
    border-radius: 7px;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.4);
    font-size: 12.5px;
  }
  .cmenu button {
    display: flex;
    justify-content: space-between;
    width: 100%;
    padding: 5px 9px;
    border: 0;
    border-radius: 4px;
    background: none;
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .cmenu button:hover:not(:disabled) {
    background: var(--hover-strong);
  }
  .cmenu button:disabled {
    color: var(--text-faint);
    cursor: default;
  }
  .cmenu kbd {
    color: var(--text-faint);
    font-size: 11px;
  }
  .msep {
    height: 1px;
    margin: 4px 2px;
    background: var(--border);
  }
  .cell.sel {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    background: var(--accent-bg);
  }
  .rownum {
    color: var(--text-faint);
    text-align: right;
    background: var(--panel);
    position: sticky;
    left: 0;
    z-index: 1;
    border-right: 1px solid var(--border-strong);
  }
  .corner {
    z-index: 3;
  }
  .hcell {
    position: relative;
    display: flex;
    align-items: center;
    gap: 6px;
    height: 28px;
    background: none;
    border: none;
    border-right: 1px solid var(--grid-line);
    font: inherit;
    text-align: left;
    cursor: pointer;
    padding: 0 8px;
  }
  .hcell:hover {
    background: var(--hover);
  }
  .hname {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    font-family: var(--sans);
  }
  .htype {
    color: var(--text-faint);
    font-size: 10.5px;
    overflow: hidden;
    text-overflow: ellipsis;
    flex-shrink: 10;
  }
  .sort {
    font-size: 9px;
    color: var(--accent);
  }
  .resize {
    position: absolute;
    right: -3px;
    top: 0;
    bottom: 0;
    width: 7px;
    cursor: col-resize;
    z-index: 1;
  }
  .resize:hover {
    background: var(--accent);
    opacity: 0.5;
  }
  .empty {
    padding: 16px;
    color: var(--text-muted);
  }
</style>
