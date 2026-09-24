<script lang="ts">
  import type { Result } from './state.svelte';
  import type { ColumnRef } from '../../bindings/dbird/internal/dbx/models';
  import { Clipboard } from '@wailsio/runtime';
  import { QueryService } from '../../bindings/dbird';
  import { app } from './state.svelte';
  import { editsFor, resultConnection } from './gridedits.svelte';

  let {
    result,
    serverSort = null,
    onsort,
  }: {
    result: Result;
    // Set when sorting runs the query again on the server (see app.browse).
    serverSort?: { col: number; desc: boolean } | null;
    onsort?: (col: number) => void;
  } = $props();

  const ROW_H = 24;
  const OVERSCAN = 12;

  let scrollTop = $state(0);
  let viewportH = $state(400);
  // Selection in display positions: the anchor stays put while the focus
  // moves with Shift+arrows or Shift+click.
  let anchor = $state<{ r: number; c: number } | null>(null);
  let focus = $state<{ r: number; c: number } | null>(null);
  let dragging = false;
  let sort = $state<{ col: number; dir: 1 | -1 } | null>(null);
  let widths = $state<number[]>([]);
  let viewport: HTMLDivElement | undefined = $state();
  // The value viewer; with editable set it can change the cell.
  let viewing = $state<{ r: number; c: number; draft: string; isNull: boolean; editing: boolean } | null>(null);
  let pretty = $state(true);
  // The cell being edited inline: r is the row index (see edits.value).
  let editing = $state<{ r: number; c: number; text: string; wasNull: boolean } | null>(null);
  let suggestions = $state<string[]>([]);
  let suggestAt = $state(-1);
  let menu = $state<{ x: number; y: number } | null>(null);

  const edits = $derived(editsFor(result));
  const columns = $derived(result.columns ?? []);
  const rawRows = $derived((result.rows ?? []) as (string | null)[][]);
  const editable = $derived(!!result.editable);
  const connId = $derived(resultConnection(result));
  const driver = $derived(app.connection(connId)?.driver ?? '');
  const col = (c: number) => edits.column(c);
  const canEdit = (c: number) => edits.canEdit(c);
  const isKey = (c: number) => !!result.editable?.key?.includes(c);

  // Reset view state when a new result arrives.
  $effect.pre(() => {
    void result;
    anchor = focus = null;
    editing = null;
    menu = null;
    viewing = null;
    sort = null;
    scrollTop = 0;
    if (viewport) viewport.scrollTop = 0;
  });

  // Initial column widths from header and a sample of the data.
  $effect.pre(() => {
    const sample = rawRows.slice(0, 200);
    widths = columns.map((c, i) => {
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

  // Row indices in display order: existing rows (sorted here unless the
  // server sorts), then new rows. Sorting uses the values as queried, so rows
  // don't jump around while being edited.
  const order = $derived.by(() => {
    void edits.version;
    const idx = rawRows.map((_, i) => i);
    if (sort) {
      const { col: c, dir } = sort;
      idx.sort((a, b) => dir * compare(rawRows[a][c], rawRows[b][c]));
    }
    for (let i = 0; i < edits.added.length; i++) idx.push(rawRows.length + i);
    return idx;
  });
  const cell = (r: number, c: number) => (void edits.version, edits.value(r, c));
  const rowValues = (r: number) => columns.map((_, c) => cell(r, c) ?? null);

  const first = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN));
  const last = $derived(Math.min(order.length, Math.ceil((scrollTop + viewportH) / ROW_H) + OVERSCAN));
  const visible = $derived(order.slice(first, last));
  const template = $derived(`52px ${widths.map((w) => w + 'px').join(' ')}`);
  const totalW = $derived(52 + widths.reduce((a, b) => a + b, 0));

  const range = $derived.by(() => {
    if (!anchor || !focus) return null;
    return {
      r0: Math.min(anchor.r, focus.r),
      r1: Math.max(anchor.r, focus.r),
      c0: Math.min(anchor.c, focus.c),
      c1: Math.max(anchor.c, focus.c),
    };
  });
  const inRange = (pos: number, c: number) =>
    !!range && pos >= range.r0 && pos <= range.r1 && c >= range.c0 && c <= range.c1;
  // Row indices of the selected rows.
  function selectedRows(): number[] {
    if (!range) return [];
    return order.slice(range.r0, range.r1 + 1);
  }

  function toggleSort(i: number) {
    if (onsort) return onsort(i);
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

  function tsvCell(v: string | null | undefined): string {
    if (v == null) return '';
    return /[\t\n"]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v;
  }

  // Splits tab-separated text (as copied from a spreadsheet) into rows of cells.
  function parseTSV(text: string): string[][] {
    const rows: string[][] = [];
    let row: string[] = [];
    let cur = '';
    let quoted = false;
    for (let i = 0; i < text.length; i++) {
      const ch = text[i];
      if (quoted) {
        if (ch === '"' && text[i + 1] === '"') {
          cur += '"';
          i++;
        } else if (ch === '"') quoted = false;
        else cur += ch;
      } else if (ch === '"' && cur === '') quoted = true;
      else if (ch === '\t') {
        row.push(cur);
        cur = '';
      } else if (ch === '\n' || ch === '\r') {
        if (ch === '\r' && text[i + 1] === '\n') i++;
        row.push(cur);
        rows.push(row);
        row = [];
        cur = '';
      } else cur += ch;
    }
    if (cur !== '' || row.length) {
      row.push(cur);
      rows.push(row);
    }
    return rows;
  }

  async function copy(text: string, what: string) {
    try {
      await Clipboard.SetText(text);
      app.toast(`Copied ${what}`);
    } catch {
      try {
        await navigator.clipboard.writeText(text);
        app.toast(`Copied ${what}`);
      } catch {
        app.toast('Copy failed', 'error');
      }
    }
  }

  async function readClipboard(): Promise<string> {
    try {
      // Fall back to the browser clipboard if the runtime doesn't answer
      // or has nothing (it only works in the desktop app).
      const timeout = new Promise<never>((_, reject) => setTimeout(() => reject(new Error('timeout')), 1500));
      const text = await Promise.race([Clipboard.Text(), timeout]);
      if (text) return text;
    } catch {
      /* use the browser clipboard */
    }
    return navigator.clipboard.readText();
  }

  export function copyAll() {
    const head = columns.map((c) => tsvCell(c.name)).join('\t');
    const body = order.map((r) => rowValues(r).map(tsvCell).join('\t')).join('\n');
    copy(head + '\n' + body, `${order.length} rows`);
  }

  function copySelection() {
    if (!range) return;
    const rows = selectedRows();
    const text = rows
      .map((r) => {
        const vals = [];
        for (let c = range.c0; c <= range.c1; c++) vals.push(tsvCell(cell(r, c)));
        return vals.join('\t');
      })
      .join('\n');
    const n = (range.r1 - range.r0 + 1) * (range.c1 - range.c0 + 1);
    copy(text, n === 1 ? 'value' : `${n} cells`);
  }

  // Pastes tab-separated text at the selection: one value fills every
  // selected cell, a block goes from the top-left cell, adding rows at the end.
  async function paste() {
    if (!editable || !range) return;
    let text: string;
    try {
      text = await readClipboard();
    } catch {
      app.toast("Couldn't read the clipboard", 'error');
      return;
    }
    const block = parseTSV(text.replace(/\r?\n$/, ''));
    if (block.length === 0) return;
    const changes: { r: number; c: number; v: string | null }[] = [];
    let skipped = 0;
    const put = (r: number, c: number, v: string) => {
      if (c >= columns.length) return;
      if (!canEdit(c)) skipped++;
      else changes.push({ r, c, v });
    };
    if (block.length === 1 && block[0].length === 1) {
      for (const r of selectedRows()) for (let c = range.c0; c <= range.c1; c++) put(r, c, block[0][0]);
    } else {
      const extra = range.r0 + block.length - order.length;
      if (extra > 0) edits.addRows(Array.from({ length: extra }, () => ({})));
      const rows = order.slice(range.r0, range.r0 + block.length);
      // New rows were just added after the existing ones.
      while (rows.length < block.length) rows.push(edits.total - (block.length - rows.length));
      block.forEach((vals, i) => vals.forEach((v, j) => put(rows[i], range.c0 + j, v)));
    }
    edits.setMany(changes);
    if (skipped) app.toast(`${skipped} value${skipped === 1 ? '' : 's'} not pasted into read-only columns`);
  }

  // Copies the first selected row's values down over the rest of the selection.
  function fillDown() {
    if (!range || range.r1 === range.r0) return;
    const rows = selectedRows();
    const changes = [];
    for (let c = range.c0; c <= range.c1; c++) {
      const v = cell(rows[0], c) ?? null;
      for (const r of rows.slice(1)) changes.push({ r, c, v });
    }
    edits.setMany(changes);
  }

  function select(pos: number, c: number, extend = false) {
    focus = { r: pos, c };
    if (!extend || !anchor) anchor = { r: pos, c };
    scrollIntoView(pos);
  }

  function scrollIntoView(pos: number) {
    if (!viewport) return;
    const top = pos * ROW_H;
    if (top < viewport.scrollTop) viewport.scrollTop = top;
    else if (top + ROW_H * 2 > viewport.scrollTop + viewportH) viewport.scrollTop = top + ROW_H * 2 - viewportH;
  }

  function moveSelection(dr: number, dc: number, extend = false) {
    if (!focus) return;
    const nr = Math.max(0, Math.min(order.length - 1, focus.r + dr));
    const nc = Math.max(0, Math.min(columns.length - 1, focus.c + dc));
    select(nr, nc, extend);
  }

  const truthy = (v: string | null | undefined) => v != null && ['t', 'true', '1', 'yes', 'on', 'y'].includes(v.toLowerCase());

  function view(r: number, c: number, edit = false) {
    const v = cell(r, c) ?? null;
    viewing = { r, c, draft: v ?? '', isNull: v == null, editing: edit && canEdit(c) };
  }

  // Starts editing a cell: text replaces its value when typing on a selected
  // cell. Booleans toggle, JSON opens in the viewer.
  function startEdit(r: number, c: number, text?: string) {
    if (!canEdit(c)) {
      const why = !result.editable
        ? result.readOnly || 'not editable'
        : col(c)?.source
          ? `${columns[c].name} can't be edited here`
          : `${columns[c].name} isn't a table column`;
      app.toast(`Read-only: ${why}`);
      return;
    }
    if (edits.isDeleted(r)) return;
    const kind = col(c)?.kind;
    const v = cell(r, c);
    if (kind === 'bool' && text == null) {
      const on = v == null ? false : truthy(v);
      const style = v == null ? (driver === 'postgres' ? 'tf' : '10') : /^[01]$/.test(v) ? '10' : 'tf';
      edits.set(r, c, style === '10' ? (on ? '0' : '1') : on ? 'false' : 'true');
      return;
    }
    if (kind === 'json' && text == null) return view(r, c, true);
    editing = { r, c, text: text ?? v ?? '', wasNull: v == null && text == null };
    suggestions = [];
    suggestAt = -1;
    if (col(c)?.ref) loadSuggestions();
  }

  let suggestTimer: ReturnType<typeof setTimeout> | undefined;
  function loadSuggestions() {
    clearTimeout(suggestTimer);
    const e = editing;
    const ref = e ? col(e.c)?.ref : null;
    if (!e || !ref || !connId) return;
    suggestTimer = setTimeout(async () => {
      try {
        const vals = (await QueryService.ReferencedValues(connId, ref, e.text)) ?? [];
        if (editing === e || (editing && editing.r === e.r && editing.c === e.c)) {
          suggestions = vals;
          suggestAt = -1;
        }
      } catch {
        suggestions = [];
      }
    }, 150);
  }

  function commitEdit(value?: string) {
    if (!editing) return;
    const { r, c, wasNull } = editing;
    const text = value ?? editing.text;
    editing = null;
    suggestions = [];
    // An untouched NULL stays NULL rather than becoming ''.
    if (!(wasNull && text === '' && value == null)) edits.set(r, c, text);
    viewport?.focus();
  }

  function cancelEdit() {
    editing = null;
    suggestions = [];
    viewport?.focus();
  }

  function onEditKey(e: KeyboardEvent) {
    e.stopPropagation();
    if (suggestions.length && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      e.preventDefault();
      suggestAt = Math.max(-1, Math.min(suggestions.length - 1, suggestAt + (e.key === 'ArrowDown' ? 1 : -1)));
      return;
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.altKey) {
      e.preventDefault();
      commitEdit(suggestAt >= 0 ? suggestions[suggestAt] : undefined);
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
      e.preventDefault();
      commitEdit();
      viewport?.dispatchEvent(new KeyboardEvent('keydown', { key: 's', ctrlKey: true, bubbles: true }));
    }
  }

  function applyViewer() {
    if (!viewing) return;
    const { r, c, draft, isNull } = viewing;
    edits.set(r, c, isNull ? null : draft);
    viewing = null;
    viewport?.focus();
  }

  const viewerJSONError = $derived.by(() => {
    if (!viewing?.editing || viewing.isNull || col(viewing.c)?.kind !== 'json') return '';
    try {
      JSON.parse(viewing.draft);
      return '';
    } catch (e) {
      return (e as Error).message;
    }
  });

  const viewText = $derived.by(() => {
    if (!viewing) return '';
    if (viewing.isNull) return 'NULL';
    const v = viewing.draft;
    if (pretty && /^\s*[[{]/.test(v)) {
      try {
        return JSON.stringify(JSON.parse(v), null, 2);
      } catch {
        /* not JSON */
      }
    }
    return v;
  });

  function onkeydown(e: KeyboardEvent) {
    const mod = e.ctrlKey || e.metaKey;
    const key = e.key.toLowerCase();
    if (mod && key === 'z' && editable) {
      e.preventDefault();
      if (e.shiftKey) edits.redo();
      else edits.undo();
      return;
    }
    if (mod && key === 'y' && editable) {
      e.preventDefault();
      edits.redo();
      return;
    }
    if (mod && key === 'a') {
      e.preventDefault();
      anchor = { r: 0, c: 0 };
      focus = { r: order.length - 1, c: columns.length - 1 };
      return;
    }
    if (!focus) return;
    const { r: pos, c } = focus;
    const r = order[pos];
    const move = (dr: number, dc: number) => {
      moveSelection(dr, dc, e.shiftKey);
      e.preventDefault();
    };
    switch (e.key) {
      case 'ArrowDown': return move(1, 0);
      case 'ArrowUp': return move(-1, 0);
      case 'ArrowLeft': return move(0, -1);
      case 'ArrowRight': return move(0, 1);
      case 'PageDown': return move(Math.floor(viewportH / ROW_H), 0);
      case 'PageUp': return move(-Math.floor(viewportH / ROW_H), 0);
      case 'Home': return move(mod ? -pos : 0, mod ? 0 : -c);
      case 'End': return move(mod ? order.length : 0, mod ? 0 : columns.length);
    }
    if (mod && key === 'c') {
      e.preventDefault();
      if (e.shiftKey) copy(rowValues(r).map(tsvCell).join('\t'), 'row');
      else copySelection();
      return;
    }
    if (mod && key === 'v' && editable) {
      e.preventDefault();
      paste();
      return;
    }
    if (mod && key === 'd' && editable) {
      e.preventDefault();
      fillDown();
      return;
    }
    if (e.key === 'Delete' && editable) {
      e.preventDefault();
      edits.toggleDelete(selectedRows());
      return;
    }
    if (e.key === 'Enter' && e.shiftKey) {
      e.preventDefault();
      view(r, c, editable);
      return;
    }
    if (e.key === 'F2' || (e.key === 'Enter' && editable)) {
      e.preventDefault();
      startEdit(r, c);
      return;
    }
    if (e.key === 'Enter' || (e.key === ' ' && !canEdit(c))) {
      e.preventDefault();
      view(r, c);
      return;
    }
    if (e.key === ' ' && col(c)?.kind === 'bool') {
      e.preventDefault();
      startEdit(r, c);
      return;
    }
    if (e.key === 'Backspace' && canEdit(c) && col(c)?.kind !== 'bool') {
      e.preventDefault();
      startEdit(r, c, '');
      return;
    }
    // Typing on an editable cell starts editing it, like a spreadsheet.
    if (!mod && !e.altKey && e.key.length === 1 && canEdit(c) && !['bool', 'enum', 'json'].includes(col(c)?.kind ?? '')) {
      e.preventDefault();
      startEdit(r, c, e.key);
    }
  }

  function cellDown(e: MouseEvent, pos: number, c: number) {
    if (e.button !== 0) return;
    select(pos, c, e.shiftKey);
    dragging = true;
    const up = () => {
      dragging = false;
      window.removeEventListener('mouseup', up);
    };
    window.addEventListener('mouseup', up);
  }

  function rowDown(e: MouseEvent, pos: number) {
    if (e.button !== 0) return;
    if (!e.shiftKey || !anchor) anchor = { r: pos, c: 0 };
    focus = { r: pos, c: columns.length - 1 };
    anchor = { r: anchor.r, c: 0 };
  }

  function openMenu(e: MouseEvent, pos: number, c: number) {
    e.preventDefault();
    if (!inRange(pos, c)) select(pos, c);
    menu = { x: e.clientX, y: e.clientY };
  }

  // Closes the context menu and runs fn.
  function menuAction(fn: () => void) {
    menu = null;
    fn();
  }

  // Adds a new row and selects its first editable cell.
  export function addRow() {
    if (!editable) return;
    edits.addRows();
    // Start at the first column that needs a value, else the first one
    // without a default.
    const pick = (ok: (i: number) => boolean) => columns.findIndex((_, i) => canEdit(i) && ok(i));
    const required = pick((i) => !col(i)?.nullable && !col(i)?.hasDefault);
    const noDefault = pick((i) => !col(i)?.hasDefault);
    const c = Math.max(0, required >= 0 ? required : noDefault >= 0 ? noDefault : pick(() => true));
    select(order.length - 1, c);
    viewport?.focus();
  }

  function openRef(ref: ColumnRef, value: string | null | undefined) {
    if (value == null || !connId) return;
    app.openReference(connId, ref, value);
  }

  // Text and style of a cell's value.
  function display(r: number, c: number): { text: string; cls: string } {
    const v = cell(r, c);
    const meta = col(c);
    if (v === undefined) {
      if (!meta?.name) return { text: '', cls: 'placeholder' };
      const required = !meta.nullable && !meta.hasDefault;
      return { text: required ? 'required' : 'default', cls: required ? 'placeholder required' : 'placeholder' };
    }
    if (v === null) return { text: 'NULL', cls: 'null' };
    if (meta?.kind === 'bool') return { text: v, cls: truthy(v) ? 'bool on' : 'bool' };
    return { text: v.length > 300 ? v.slice(0, 300) + '…' : v, cls: '' };
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
    aria-multiselectable="true"
    {onkeydown}
  >
    <div class="head" style:grid-template-columns={template} style:width="{totalW}px">
      <div class="cell rownum corner">#</div>
      {#each columns as c, i (i)}
        {@const meta = col(i)}
        {@const sorted = serverSort ? (serverSort.col === i + 1 ? (serverSort.desc ? -1 : 1) : 0) : sort?.col === i ? sort.dir : 0}
        <button
          class={['cell', 'hcell', isKey(i) && 'key']}
          title="{c.name} ({meta?.type || c.type}){isKey(i) ? ` · ${result.editable?.keyName}` : ''}{meta?.ref
            ? ` · references ${meta.ref.table}.${meta.ref.column}`
            : ''}{editable && !meta?.name ? ' · read-only' : ''}{onsort ? ' · click to sort on the server' : ''}"
          onclick={() => toggleSort(i)}
        >
          <span class="hname">{c.name}</span>
          <span class="htype">{c.type}</span>
          {#if meta?.ref}<span class="hflag" aria-label="foreign key">↗</span>{/if}
          {#if sorted}<span class="sort">{sorted === 1 ? '▲' : '▼'}</span>{/if}
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
        {@const added = edits.isAdded(r)}
        {@const gone = edits.isDeleted(r)}
        <div
          class={[
            'row',
            pos % 2 === 1 && 'odd',
            focus?.r === pos && 'selrow',
            (edits.cells[r] || added) && 'dirty',
            added && 'added',
            gone && 'deleted',
            editing?.r === r && 'editrow',
          ]}
          style:grid-template-columns={template}
          style:transform="translateY({pos * ROW_H}px)"
        >
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="cell rownum" onmousedown={(e) => rowDown(e, pos)} oncontextmenu={(e) => openMenu(e, pos, 0)}>
            {added ? '+' : pos + 1}
          </div>
          {#each columns as _, c (c)}
            {#if editing?.r === r && editing.c === c}
              {@const meta = col(c)}
              <div class="cell editing">
                {#if meta?.kind === 'enum'}
                  <select
                    value={editing.text}
                    onchange={(e) => commitEdit(e.currentTarget.value)}
                    onkeydown={onEditKey}
                    onblur={() => cancelEdit()}
                    {@attach (el) => el.focus()}
                  >
                    {#if editing.wasNull}<option value="" disabled>NULL</option>{/if}
                    {#each meta.enum ?? [] as opt (opt)}<option value={opt}>{opt}</option>{/each}
                  </select>
                {:else if meta?.kind === 'date'}
                  <input
                    type="date"
                    bind:value={editing.text}
                    onkeydown={onEditKey}
                    onblur={() => commitEdit()}
                    {@attach (el) => el.focus()}
                  />
                {:else}
                  <!-- svelte-ignore a11y_autofocus -->
                  <textarea
                    bind:value={editing.text}
                    placeholder={editing.wasNull ? 'NULL' : ''}
                    rows="1"
                    spellcheck="false"
                    oninput={() => meta?.ref && loadSuggestions()}
                    onkeydown={onEditKey}
                    onblur={() => commitEdit()}
                    {@attach (el) => {
                      el.focus();
                      el.setSelectionRange(el.value.length, el.value.length);
                    }}
                  ></textarea>
                  {#if suggestions.length}
                    <div class="suggest" role="listbox">
                      {#each suggestions as s, i (s)}
                        <button
                          role="option"
                          aria-selected={i === suggestAt}
                          class={[i === suggestAt && 'on']}
                          onmousedown={(e) => (e.preventDefault(), commitEdit(s))}>{s}</button
                        >
                      {/each}
                    </div>
                  {/if}
                {/if}
              </div>
            {:else}
              {@const d = display(r, c)}
              {@const meta = col(c)}
              {@const v = cell(r, c)}
              <div
                class={[
                  'cell',
                  d.cls,
                  edits.edited(r, c) && !added && 'changed',
                  inRange(pos, c) && 'inrange',
                  focus?.r === pos && focus?.c === c && 'sel',
                ]}
                role="gridcell"
                tabindex="-1"
                title={v === undefined ? d.text : (v ?? 'NULL')}
                onmousedown={(e) => cellDown(e, pos, c)}
                onmouseenter={() => dragging && select(pos, c, true)}
                ondblclick={() => (editable && canEdit(c) ? startEdit(r, c) : view(r, c))}
                oncontextmenu={(e) => openMenu(e, pos, c)}
              >
                {#if d.cls.startsWith('bool')}<span class="chk" aria-hidden="true"></span>{/if}{d.text}
                {#if meta?.ref && v != null && !added}
                  <button
                    class="fk"
                    title="Open the {meta.ref.table} row with {meta.ref.column} = {v}"
                    onmousedown={(e) => e.stopPropagation()}
                    onclick={(e) => (e.stopPropagation(), openRef(meta.ref!, v))}>↗</button
                  >
                {/if}
              </div>
            {/if}
          {/each}
        </div>
      {/each}
    </div>
  </div>
{/if}

{#if menu && focus}
  {@const m = menu}
  {@const r = order[focus.r]}
  {@const c = focus.c}
  {@const meta = col(c)}
  {@const rows = selectedRows()}
  {@const allGone = rows.length > 0 && rows.every((x) => edits.isDeleted(x))}
  {@const multi = !!range && (range.r0 !== range.r1 || range.c0 !== range.c1)}
  <div class="mbackdrop" role="presentation" onmousedown={() => (menu = null)} oncontextmenu={(e) => (e.preventDefault(), (menu = null))}></div>
  <div class="cmenu" style:left="{m.x}px" style:top="{m.y}px" role="menu">
    {#if editable}
      {#if canEdit(c)}
        <button role="menuitem" onclick={() => menuAction(() => startEdit(r, c))}>Edit <kbd>F2</kbd></button>
        {#if meta?.kind === 'json' || meta?.kind === 'text'}
          <button role="menuitem" onclick={() => menuAction(() => view(r, c, true))}>Edit in viewer <kbd>Shift+Enter</kbd></button>
        {/if}
        <button
          role="menuitem"
          onclick={() => menuAction(() => edits.setMany(rows.map((x) => ({ r: x, c, v: null }))))}
          disabled={!meta?.nullable}>Set to NULL</button
        >
        <button role="menuitem" onclick={() => menuAction(() => edits.revert(r, c))} disabled={!edits.edited(r, c)}>Revert value</button>
        <div class="msep"></div>
      {/if}
      <button role="menuitem" onclick={() => menuAction(addRow)}>Add row</button>
      <button role="menuitem" onclick={() => menuAction(() => edits.toggleDelete(rows))}>
        {allGone ? 'Restore' : 'Delete'}
        {rows.length > 1 ? `${rows.length} rows` : 'row'} <kbd>Del</kbd>
      </button>
      <div class="msep"></div>
      <button role="menuitem" onclick={() => menuAction(paste)}>Paste <kbd>Ctrl+V</kbd></button>
      {#if multi}
        <button role="menuitem" onclick={() => menuAction(fillDown)}>Fill down <kbd>Ctrl+D</kbd></button>
      {/if}
    {/if}
    <button role="menuitem" onclick={() => menuAction(copySelection)}>{multi ? 'Copy selection' : 'Copy value'} <kbd>Ctrl+C</kbd></button>
    <button role="menuitem" onclick={() => menuAction(() => copy(rowValues(r).map(tsvCell).join('\t'), 'row'))}>Copy row</button>
    <button role="menuitem" onclick={() => menuAction(() => view(r, c))}>View value</button>
    {#if meta?.ref && cell(r, c) != null && !edits.isAdded(r)}
      <button role="menuitem" onclick={() => menuAction(() => openRef(meta.ref!, cell(r, c)))}>Open {meta.ref.table} row</button>
    {/if}
  </div>
{/if}

{#if viewing}
  {@const v = viewing}
  {@const meta = col(v.c)}
  <div class="vbackdrop" role="presentation" onmousedown={(e) => e.target === e.currentTarget && (viewing = null)}>
    <div
      class="viewer"
      role="dialog"
      aria-label="Value of {columns[v.c].name}"
      tabindex="-1"
      onkeydown={(e) => {
        e.stopPropagation();
        if (e.key === 'Escape') {
          viewing = null;
          viewport?.focus();
        } else if (e.key === 'Enter' && (e.ctrlKey || e.metaKey) && v.editing && !viewerJSONError) {
          e.preventDefault();
          applyViewer();
        }
      }}
      {@attach (el) => el.focus()}
    >
      <header>
        <strong>{columns[v.c].name}</strong>
        <span class="vtype">{meta?.type || columns[v.c].type}</span>
        <span class="vlen">{v.isNull ? 'NULL' : `${v.draft.length.toLocaleString()} chars`}</span>
        <span class="vspacer"></span>
        {#if v.editing}
          {#if meta?.kind === 'json' && !v.isNull}
            <button
              class="btn sm"
              disabled={!!viewerJSONError}
              onclick={() => viewing && (viewing.draft = JSON.stringify(JSON.parse(viewing.draft), null, 2))}>Format</button
            >
          {/if}
          {#if meta?.nullable}
            <label class="vpretty"><input type="checkbox" bind:checked={v.isNull} /> NULL</label>
          {/if}
        {:else}
          <label class="vpretty"><input type="checkbox" bind:checked={pretty} /> Format JSON</label>
          {#if canEdit(v.c) && !edits.isDeleted(v.r)}
            <button class="btn sm" onclick={() => viewing && (viewing.editing = true)}>Edit</button>
          {/if}
        {/if}
        <button class="btn sm" onclick={() => copy(v.isNull ? '' : v.draft, 'value')}>Copy</button>
        {#if v.editing}
          <button class="btn sm" onclick={() => (viewing = null)}>Cancel</button>
          <button class="btn sm primary" disabled={!!viewerJSONError} onclick={applyViewer}>Apply <kbd>Ctrl+Enter</kbd></button>
        {:else}
          <button class="btn sm" onclick={() => (viewing = null)}>Close</button>
        {/if}
      </header>
      {#if v.editing}
        <textarea class="vedit" bind:value={v.draft} disabled={v.isNull} spellcheck="false" {@attach (el) => el.focus()}></textarea>
        {#if viewerJSONError}<div class="verr">Invalid JSON: {viewerJSONError}</div>{/if}
      {:else}
        <pre class={[v.isNull && 'null']}>{viewText}</pre>
      {/if}
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
  .chk {
    display: inline-block;
    width: 10px;
    height: 10px;
    margin-right: 6px;
    vertical-align: -1px;
    border: 1.5px solid var(--text-faint);
    border-radius: 2px;
  }
  .bool.on .chk {
    border-color: var(--accent);
    background: var(--accent);
    box-shadow: inset 0 0 0 1.5px var(--grid-bg);
  }
  .row.editrow {
    z-index: 3;
  }
  .cell.inrange {
    background: color-mix(in srgb, var(--accent) 12%, transparent);
  }
  .row.added .rownum {
    color: var(--success);
    box-shadow: inset 3px 0 var(--success);
  }
  .row.deleted .cell:not(.rownum) {
    text-decoration: line-through;
    opacity: 0.45;
  }
  .row.deleted .rownum {
    color: var(--danger);
    box-shadow: inset 3px 0 var(--danger);
  }
  .rownum {
    cursor: default;
  }
  .cell.placeholder {
    color: var(--text-faint);
    font-style: italic;
  }
  .cell.placeholder.required {
    color: var(--danger);
  }
  .hcell.key .hname {
    text-decoration: underline;
    text-decoration-color: var(--accent);
    text-underline-offset: 3px;
  }
  .hflag {
    color: var(--accent);
    font-size: 11px;
  }
  .cell {
    position: relative;
  }
  .fk {
    position: absolute;
    right: 2px;
    top: 3px;
    height: 18px;
    padding: 0 5px;
    border: 1px solid var(--border-strong);
    border-radius: 4px;
    background: var(--panel);
    color: var(--accent);
    font-size: 11px;
    line-height: 16px;
    cursor: pointer;
    display: none;
  }
  .cell:hover .fk {
    display: block;
  }
  .cell.editing select,
  .cell.editing input {
    display: block;
    width: 100%;
    height: 24px;
    margin: 0;
    padding: 0 5px;
    border: 2px solid var(--accent);
    border-radius: 0;
    background: var(--input-bg);
    color: var(--text);
    font: inherit;
    outline: none;
  }
  .suggest {
    position: absolute;
    left: 0;
    top: 24px;
    min-width: 100%;
    max-height: 220px;
    overflow: auto;
    background: var(--panel);
    border: 1px solid var(--border-strong);
    border-radius: 0 0 6px 6px;
    box-shadow: 0 10px 24px rgba(0, 0, 0, 0.35);
    z-index: 5;
  }
  .suggest button {
    display: block;
    width: 100%;
    padding: 3px 8px;
    border: 0;
    background: none;
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
    white-space: nowrap;
  }
  .suggest button:hover,
  .suggest button.on {
    background: var(--accent-bg);
  }
  .vedit {
    flex: 1;
    min-height: 260px;
    margin: 0;
    padding: 12px 14px;
    border: 0;
    background: var(--input-bg);
    color: var(--text);
    font-family: var(--mono);
    font-size: 12.5px;
    resize: none;
    outline: none;
  }
  .verr {
    padding: 6px 14px;
    color: var(--danger);
    font-size: 12px;
    border-top: 1px solid var(--border);
  }
  .viewer kbd {
    margin-left: 4px;
    opacity: 0.75;
    font-size: 10.5px;
  }
</style>
