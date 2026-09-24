<script lang="ts">
  import ResultGrid from './ResultGrid.svelte';
  import { app, type Result, type Tab, type TabRuntime } from './state.svelte';
  import { browseState, editsFor } from './gridedits.svelte';

  let { rt, results, name, tab }: { rt: TabRuntime; results: Result[]; name: string; tab: Tab } = $props();

  let grid: ResultGrid | undefined = $state();
  let preview = $state<{ statements: string[]; error: string } | null>(null);
  let loadingMore = $state(false);

  const current = $derived(results[rt.activeResult]);
  const edits = $derived(current?.hasResultSet ? editsFor(current) : null);
  // Filter and sort of a result that runs on the server.
  const browse = $derived(current?.browse ? browseState(current) : null);
  let filterText = $state('');
  // Why the last filter or sort failed; the previous rows stay visible.
  let browseError = $state('');
  $effect.pre(() => {
    filterText = browse?.filter ?? '';
  });

  async function rerun(patch: Parameters<typeof app.browse>[2]) {
    if (!current) return;
    browseError = await app.browse(tab, current, patch);
  }
  const serverSort = $derived(browse && browse.orderBy > 0 ? { col: browse.orderBy, desc: browse.desc } : null);
  const canLoadMore = $derived.by(() => {
    if (!current?.browse || !edits || !browse) return false;
    void edits.version;
    if (browse.exhausted) return false;
    const page = current.browse.limit || app.maxRows;
    return current.truncated || (edits.base > 0 && edits.base % page === 0);
  });

  function applyFilter() {
    if (!current || (filterText.trim() === (browse?.filter ?? '') && !browseError)) return;
    rerun({ filter: filterText.trim() });
  }

  // Header click: ascending, descending, then back to the query's own order.
  function sortOn(col: number) {
    if (!current || !browse) return;
    const c = col + 1;
    if (browse.orderBy !== c) rerun({ orderBy: c, desc: false });
    else if (!browse.desc) rerun({ orderBy: c, desc: true });
    else rerun({ orderBy: 0, desc: false });
  }

  async function loadMore() {
    if (!current) return;
    loadingMore = true;
    try {
      await app.loadMore(tab, current);
    } finally {
      loadingMore = false;
    }
  }

  async function showPreview() {
    if (!edits) return;
    const invalid = edits.validate();
    try {
      preview = { statements: await edits.preview(), error: invalid };
    } catch (e) {
      preview = { statements: [], error: String((e as Error)?.message ?? e) };
    }
  }

  function changeSummary(): string {
    if (!edits) return '';
    const parts = [];
    if (edits.editedCount) parts.push(`${edits.editedCount} edited cell${edits.editedCount === 1 ? '' : 's'}`);
    if (edits.added.length) parts.push(`${edits.added.length} new row${edits.added.length === 1 ? '' : 's'}`);
    if (edits.deletedCount) parts.push(`${edits.deletedCount} deleted row${edits.deletedCount === 1 ? '' : 's'}`);
    return parts.join(', ');
  }

  // Ctrl+S inside the results saves edited cells (elsewhere it saves the script).
  function onkeydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && !e.shiftKey && e.key.toLowerCase() === 's' && edits && edits.count > 0) {
      e.preventDefault();
      e.stopPropagation();
      edits.save();
    }
  }

  // Ticks while a query runs, for the elapsed-time display.
  let now = $state(Date.now());
  $effect(() => {
    if (!rt.running) return;
    now = Date.now();
    const t = setInterval(() => (now = Date.now()), 100);
    return () => clearInterval(t);
  });

  function elapsed(ms: number): string {
    const sec = Math.max(0, ms) / 1000;
    if (sec < 60) return `${sec.toFixed(1)} s`;
    const m = Math.floor(sec / 60);
    const h = Math.floor(m / 60);
    const ss = String(Math.floor(sec % 60)).padStart(2, '0');
    return h > 0 ? `${h}:${String(m % 60).padStart(2, '0')}:${ss}` : `${m}:${ss}`;
  }

  function label(i: number): string {
    const r = results[i];
    if (r.error) return `Error ${i + 1}`;
    if (r.hasResultSet) return `Result ${i + 1}`;
    return `Stmt ${i + 1}`;
  }

  function dur(ms: number): string {
    if (ms < 1) return `${ms.toFixed(2)} ms`;
    if (ms < 1000) return `${Math.round(ms)} ms`;
    return `${(ms / 1000).toFixed(2)} s`;
  }

  function summary(): string {
    const r = current;
    if (!r) return '';
    void edits?.version;
    if (r.error) return `Failed after ${dur(r.durationMs)}`;
    if (r.hasResultSet) {
      const n = r.rows?.length ?? 0;
      return `${n.toLocaleString()} row${n === 1 ? '' : 's'}${r.truncated ? ' (limit reached)' : ''} · ${dur(r.durationMs)}`;
    }
    return `${r.rowsAffected.toLocaleString()} row${r.rowsAffected === 1 ? '' : 's'} affected · ${dur(r.durationMs)}`;
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<section class="results" {onkeydown}>
  {#if rt.running}
    <div class="state"><span class="spinner"></span> Executing… <span class="elapsed">{elapsed(now - rt.startedAt)}</span></div>
  {:else if rt.error}
    <div class="state error"><pre>{rt.error}</pre></div>
  {:else if results.length === 0}
    <div class="state muted">
      Run a query with <kbd>Ctrl</kbd>+<kbd>Enter</kbd> (statement at cursor) or <kbd>Alt</kbd>+<kbd>X</kbd> (whole script).
    </div>
  {:else}
    {#if results.length > 1}
      <div class="rtabs" role="tablist">
        {#each results as r, i (i)}
          <button
            role="tab"
            aria-selected={i === rt.activeResult}
            class={['rtab', i === rt.activeResult && 'active', r.error && 'err']}
            title={r.sql}
            onclick={() => (rt.activeResult = i)}>{label(i)}</button
          >
        {/each}
      </div>
    {/if}
    <div class="content">
      {#if current?.error}
        <div class="state error">
          <pre>{current.error}</pre>
          <pre class="sql">{current.sql}</pre>
        </div>
      {:else if current?.hasResultSet}
        {#if current.browse}
          <form class="filterbar" onsubmit={(e) => (e.preventDefault(), applyFilter())}>
            <span class="where">WHERE</span>
            <input
              bind:value={filterText}
              placeholder="Filter rows with SQL, e.g. name LIKE 'A%' or accountId = &quot;jeff&quot; — press Enter"
              spellcheck="false"
              class={[browseError && 'bad']}
              oninput={() => (browseError = '')}
              onkeydown={(e) => {
                if (e.key === 'Escape' && filterText) {
                  e.stopPropagation();
                  filterText = '';
                  browseError = '';
                  if (browse?.filter) rerun({ filter: '' });
                }
              }}
            />
            {#if browse?.filter || filterText}
              <button type="button" class="link" onclick={() => ((filterText = ''), (browseError = ''), browse?.filter && rerun({ filter: '' }))}
                >Clear</button
              >
            {/if}
          </form>
          {#if browseError}
            <div class="filtererr" role="alert">{browseError}</div>
          {/if}
        {/if}
        <div class="gridwrap">
          <ResultGrid bind:this={grid} result={current} {serverSort} onsort={current.browse ? sortOn : undefined} />
        </div>
      {:else if current}
        <div class="state">
          <div class="ok">✓ {current.rowsAffected.toLocaleString()} row(s) affected</div>
          <pre class="sql">{current.sql}</pre>
        </div>
      {/if}
    </div>
    {#if edits && (edits.count > 0 || edits.error)}
      <div class="editbar" role="region" aria-label="Unsaved changes">
        <span class="dot"></span>
        <span class="what">{changeSummary()} in <code>{current?.editable?.table}</code></span>
        {#if edits.error}<span class="saveerr" title={edits.error}>{edits.error}</span>{/if}
        <span class="spacer"></span>
        <button class="btn sm" onclick={() => edits?.undo()} disabled={!edits.canUndo || edits.saving} title="Undo (Ctrl+Z)">Undo</button>
        <button class="btn sm" onclick={showPreview} disabled={edits.saving || edits.count === 0} title="Show the SQL that Save runs">SQL</button>
        <button class="btn sm" onclick={() => edits?.discard()} disabled={edits.saving}>Discard</button>
        <button class="btn sm primary" onclick={() => edits?.save()} disabled={edits.saving || edits.count === 0}>
          {edits.saving ? 'Saving…' : 'Save'} <kbd>Ctrl+S</kbd>
        </button>
      </div>
    {/if}
    <footer class="status">
      <span class={[current?.error && 'errtext']}>{summary()}</span>
      {#if current?.hasResultSet && !current.error}
        {#if current.editable}
          <span
            class="mode"
            title="Double-click a cell (or F2) to edit · Del marks rows for deletion · Ctrl+V pastes · Ctrl+D fills down · Ctrl+Z undoes · rows are identified by the {current
              .editable.keyName}">editable</span
          >
        {:else if current.readOnly}
          <span class="mode ro" title={current.readOnly}>read-only</span>
        {/if}
        {#if canLoadMore}
          <button class="link" onclick={loadMore} disabled={loadingMore}>{loadingMore ? 'Loading…' : 'Load more'}</button>
        {/if}
      {/if}
      <span class="spacer"></span>
      {#if current?.editable && !current.error}
        <button class="link" onclick={() => grid?.addRow()}>+ Row</button>
      {/if}
      {#if current?.hasResultSet && !current.error}
        <button class="link" onclick={() => grid?.copyAll()}>Copy as TSV</button>
        <button class="link" onclick={() => app.exportResult(current, name)}>Export CSV…</button>
      {/if}
    </footer>
  {/if}
</section>

{#if preview}
  {@const p = preview}
  <div class="pbackdrop" role="presentation" onmousedown={(e) => e.target === e.currentTarget && (preview = null)}>
    <div
      class="pdialog"
      role="dialog"
      aria-label="SQL to be saved"
      tabindex="-1"
      onkeydown={(e) => e.key === 'Escape' && (e.stopPropagation(), (preview = null))}
      {@attach (el) => el.focus()}
    >
      <header>
        <strong>SQL that Save runs</strong>
        <span class="pmeta">{p.statements.length} statement{p.statements.length === 1 ? '' : 's'} in one transaction</span>
      </header>
      {#if p.error}<div class="perr">{p.error}</div>{/if}
      <pre>{p.statements.join('\n\n')}</pre>
      <footer>
        <button class="btn sm" onclick={() => navigator.clipboard?.writeText(p.statements.join('\n\n')).then(() => app.toast('Copied SQL'))}
          >Copy</button
        >
        <span class="spacer"></span>
        <button class="btn sm" onclick={() => (preview = null)}>Close</button>
        <button
          class="btn sm primary"
          disabled={!!p.error || edits?.saving}
          onclick={async () => {
            preview = null;
            await edits?.save();
          }}>Save</button
        >
      </footer>
    </div>
  </div>
{/if}

<style>
  .results {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--grid-bg);
  }
  .filterbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 8px;
    border-bottom: 1px solid var(--border);
    background: var(--panel);
  }
  .filterbar .where {
    font-family: var(--mono);
    font-size: 11px;
    font-weight: 700;
    color: var(--syn-keyword);
  }
  .filterbar input {
    flex: 1;
    min-width: 0;
    height: 24px;
    padding: 0 8px;
    border: 1px solid var(--border);
    border-radius: 5px;
    background: var(--input-bg);
    color: var(--text);
    font-family: var(--mono);
    font-size: 12px;
  }
  .filterbar input:focus {
    outline: none;
    border-color: var(--accent);
  }
  .filterbar input.bad {
    border-color: var(--danger);
  }
  .filtererr {
    padding: 4px 10px 5px 62px;
    border-bottom: 1px solid var(--border);
    background: var(--danger-bg);
    color: var(--danger);
    font-family: var(--mono);
    font-size: 11.5px;
    white-space: pre-wrap;
  }
  .content:has(.filterbar) {
    display: flex;
    flex-direction: column;
  }
  .gridwrap {
    flex: 1;
    min-height: 0;
    height: 100%;
  }
  .pbackdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
    background: rgba(0, 0, 0, 0.45);
    display: grid;
    place-items: center;
    padding: 24px;
  }
  .pdialog {
    width: min(860px, 100%);
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
  .pdialog header,
  .pdialog footer {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    font-size: 12.5px;
  }
  .pdialog header {
    border-bottom: 1px solid var(--border);
  }
  .pdialog footer {
    border-top: 1px solid var(--border);
  }
  .pdialog .spacer {
    flex: 1;
  }
  .pmeta {
    color: var(--text-faint);
  }
  .pdialog pre {
    margin: 0;
    padding: 12px 14px;
    overflow: auto;
    font-family: var(--mono);
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-word;
    user-select: text;
  }
  .perr {
    padding: 8px 14px;
    color: var(--danger);
    font-size: 12px;
    border-bottom: 1px solid var(--border);
  }
  .editbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 10px;
    border-top: 1px solid var(--border);
    background: color-mix(in srgb, var(--syn-number) 10%, var(--panel));
    font-size: 12px;
  }
  .editbar .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--syn-number);
    flex-shrink: 0;
  }
  .editbar code {
    font-family: var(--mono);
  }
  .editbar .saveerr {
    color: var(--danger);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .editbar .spacer {
    flex: 1;
  }
  .editbar kbd {
    margin-left: 4px;
    opacity: 0.75;
    font-size: 10.5px;
  }
  .mode {
    margin-left: 8px;
    padding: 0 6px;
    border-radius: 3px;
    font-size: 10.5px;
    color: var(--success);
    background: var(--success-bg);
    cursor: default;
  }
  .mode.ro {
    color: var(--text-faint);
    background: var(--hover);
  }
  .content {
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }
  .state {
    padding: 14px 16px;
    overflow: auto;
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: 10px;
    align-items: flex-start;
  }
  .state.muted {
    color: var(--text-muted);
    display: block;
  }
  .state.error pre:first-child {
    color: var(--danger);
  }
  .state:has(.spinner) {
    flex-direction: row;
    align-items: center;
    align-content: flex-start;
    flex-wrap: wrap;
    height: auto;
    color: var(--text-muted);
  }
  pre {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-word;
    font-family: var(--mono);
    font-size: 12px;
  }
  .sql {
    color: var(--text-muted);
    border-left: 2px solid var(--border-strong);
    padding-left: 10px;
    max-height: 220px;
    overflow: auto;
  }
  .elapsed {
    font-family: var(--mono);
    font-variant-numeric: tabular-nums;
    color: var(--text);
  }
  .ok {
    color: var(--success);
  }
  .rtabs {
    display: flex;
    gap: 2px;
    padding: 4px 6px 0;
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    flex-shrink: 0;
  }
  .rtab {
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-muted);
    padding: 4px 10px;
    font-size: 12px;
    cursor: pointer;
    white-space: nowrap;
  }
  .rtab:hover {
    color: var(--text);
  }
  .rtab.active {
    color: var(--text);
    border-bottom-color: var(--accent);
  }
  .rtab.err {
    color: var(--danger);
  }
  .status {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 3px 10px;
    font-size: 11.5px;
    color: var(--text-muted);
    border-top: 1px solid var(--border);
    background: var(--panel);
    flex-shrink: 0;
  }
  .spacer {
    flex: 1;
  }
  .errtext {
    color: var(--danger);
  }
  .link {
    background: none;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: 11.5px;
    padding: 0;
  }
  .link:hover {
    text-decoration: underline;
  }
  kbd {
    font-family: var(--mono);
    font-size: 11px;
    border: 1px solid var(--border-strong);
    border-bottom-width: 2px;
    border-radius: 4px;
    padding: 0 4px;
    background: var(--panel-2);
  }
  .spinner {
    width: 14px;
    height: 14px;
    border: 2px solid var(--border-strong);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
