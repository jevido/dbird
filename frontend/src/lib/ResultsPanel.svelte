<script lang="ts">
  import ResultGrid from './ResultGrid.svelte';
  import { app, type Result, type TabRuntime } from './state.svelte';

  let { rt, results, name }: { rt: TabRuntime; results: Result[]; name: string } = $props();

  let grid: ResultGrid | undefined = $state();

  const current = $derived(results[rt.activeResult]);

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

<section class="results">
  {#if rt.running}
    <div class="state"><span class="spinner"></span> Executing…</div>
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
    <footer class="status">
      <span class={[current?.error && 'errtext']}>{summary()}</span>
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
