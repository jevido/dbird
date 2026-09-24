<script lang="ts">
  import ResultGrid from './ResultGrid.svelte';
  import { app, type Result, type TabRuntime } from './state.svelte';
  import { editsFor } from './gridedits.svelte';

  let { rt, results, name }: { rt: TabRuntime; results: Result[]; name: string } = $props();

  let grid: ResultGrid | undefined = $state();

  const current = $derived(results[rt.activeResult]);
  const edits = $derived(current?.hasResultSet ? editsFor(current) : null);

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
        <ResultGrid bind:this={grid} result={current} />
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
        <span class="what">
          {edits.count} changed cell{edits.count === 1 ? '' : 's'} in {edits.rowCount} row{edits.rowCount === 1 ? '' : 's'}
          of <code>{current?.editable?.table}</code>
        </span>
        {#if edits.error}<span class="saveerr" title={edits.error}>{edits.error}</span>{/if}
        <span class="spacer"></span>
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
          <span class="mode" title="Double-click a cell, or press F2, to edit it">editable</span>
        {:else if current.readOnly}
          <span class="mode ro" title={current.readOnly}>read-only</span>
        {/if}
      {/if}
      <span class="spacer"></span>
      {#if current?.hasResultSet && !current.error}
        <button class="link" onclick={() => grid?.copyAll()}>Copy as TSV</button>
        <button class="link" onclick={() => app.exportResult(current, name)}>Export CSV…</button>
      {/if}
    </footer>
  {/if}
</section>

<style>
  .results {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--grid-bg);
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
