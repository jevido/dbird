<script lang="ts">
  import SqlEditor from './SqlEditor.svelte';
  import ResultsPanel from './ResultsPanel.svelte';
  import { app, type Tab } from './state.svelte';

  let { tab }: { tab: Tab } = $props();

  let editor: SqlEditor | undefined = $state();
  let resultsH = $state(loadHeight());
  let pane: HTMLDivElement | undefined = $state();

  const rt = $derived(app.rt(tab.id));
  const conn = $derived(app.connection(tab.connectionId));

  function loadHeight(): number {
    const fallback = Math.round(window.innerHeight * 0.42);
    try {
      return Number(localStorage.getItem('dbird.resultsH')) || fallback;
    } catch {
      return fallback;
    }
  }

  function run(script: boolean) {
    if (!editor) return;
    editor.flash(app.execute(tab, editor.selection(), script));
  }

  function setConnection(id: string) {
    tab.connectionId = id;
    app.scheduleSave();
    if (id && !app.connected[id]) app.connect(id);
    else if (id && !app.completions[id]) app.loadCompletions(id);
  }

  function startResize(e: PointerEvent) {
    e.preventDefault();
    const startY = e.clientY;
    const startH = resultsH;
    const max = (pane?.clientHeight ?? 800) - 80;
    const move = (ev: PointerEvent) => (resultsH = Math.max(60, Math.min(max, startH - (ev.clientY - startY))));
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      try {
        localStorage.setItem('dbird.resultsH', String(resultsH));
      } catch {
        /* ignore */
      }
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  }
</script>

<div class="pane" bind:this={pane}>
  <div class="toolbar">
    <label class="connpick" style:--conn-color={conn?.color || 'var(--text-faint)'}>
      <span class={['dot', app.connected[tab.connectionId] && 'on']}></span>
      <select value={tab.connectionId} onchange={(e) => setConnection(e.currentTarget.value)} aria-label="Connection">
        <option value="">— no connection —</option>
        {#each app.connections as c (c.id)}
          <option value={c.id}>{c.name} ({c.driver})</option>
        {/each}
      </select>
    </label>

    <div class="sep"></div>

    <button class="btn primary" disabled={rt.running} onclick={() => run(false)} title="Execute statement at cursor (Ctrl+Enter)">
      <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 2.5v11l9-5.5z" fill="currentColor" /></svg>
      Run
    </button>
    <button class="btn" disabled={rt.running} onclick={() => run(true)} title="Execute script (Alt+X / Ctrl+Shift+Enter)">
      <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M2 2.5v11l6-5.5zM8 2.5v11l6-5.5z" fill="currentColor" /></svg>
      Run script
    </button>
    <button class="btn danger" disabled={!rt.running} onclick={() => app.cancel(tab.id)} title="Cancel running query">
      <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="3.5" y="3.5" width="9" height="9" rx="1" fill="currentColor" /></svg>
      Stop
    </button>

    <span class="spacer"></span>

    <button class="icon-btn" title="Open script (Ctrl+O)" aria-label="Open script" onclick={() => app.openScript()}>
      <svg viewBox="0 0 16 16"><path d="M2 4.5V13h11l1.5-6H4.5L3 13M2 4.5V3h4l1.5 1.5H12V7" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linejoin="round" /></svg>
    </button>
    {#if tab.filePath}
      <button class="icon-btn" title="Open {tab.filePath} in your default editor. Saves there show up here." aria-label="Open in external editor" onclick={() => app.openExternally(tab)}>
        <svg viewBox="0 0 16 16"><path d="M9 2.5h4.5V7M13.5 2.5 7.5 8.5M11.5 9.5v4h-9v-9h4" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </button>
    {/if}
    <button class="icon-btn" title={tab.filePath ? `Save ${tab.filePath} (Ctrl+S)` : 'Save script (Ctrl+S)'} aria-label="Save script" onclick={() => app.saveScript(tab)}>
      <svg viewBox="0 0 16 16"><path d="M3 2.5h8l2.5 2.5v8.5h-11zM5 2.5v3.5h5V2.5M5 13.5V9.5h6v4" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linejoin="round" /></svg>
    </button>
    <div class="sep"></div>

    <label class="limit" title="Maximum rows fetched per result set">
      Limit
      <select value={String(app.maxRows)} onchange={(e) => app.setMaxRows(Number(e.currentTarget.value))}>
        {#each [100, 500, 1000, 5000, 10000, 50000] as n (n)}
          <option value={String(n)}>{n.toLocaleString()}</option>
        {/each}
      </select>
    </label>
  </div>

  <div class="editor-wrap">
    <SqlEditor
      bind:this={editor}
      tabId={tab.id}
      value={tab.sql}
      dialect={conn?.driver ?? 'postgres'}
      completion={{ connId: tab.connectionId, driver: conn?.driver ?? '', setup: app.completions[tab.connectionId] }}
      fontSize={app.fontSize}
      onchange={(v) => {
        app.edited(tab, v);
        app.ensureCompletions(tab.connectionId);
      }}
      onrun={run}
    />
  </div>

  <div class="hsplit" role="separator" aria-orientation="horizontal" onpointerdown={startResize}></div>

  <div class="results-wrap" style:height="{resultsH}px">
    <ResultsPanel {rt} results={app.results[tab.id] ?? []} name={tab.title.replace(/\.sql$/i, '')} />
  </div>
</div>

<style>
  .pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .toolbar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px;
    border-bottom: 1px solid var(--border);
    background: var(--panel);
    flex-shrink: 0;
  }
  .connpick {
    display: flex;
    align-items: center;
    gap: 6px;
    padding-left: 8px;
    border: 1px solid var(--border-strong);
    border-radius: 6px;
    background: var(--input-bg);
  }
  .connpick select {
    border: none;
    background-color: transparent;
    min-width: 180px;
    max-width: 280px;
    padding: 4px 24px 4px 0;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 1.5px solid var(--conn-color);
    flex-shrink: 0;
  }
  .dot.on {
    background: var(--success);
    border-color: var(--success);
  }
  .sep {
    width: 1px;
    height: 20px;
    background: var(--border);
    margin: 0 2px;
  }
  .spacer {
    flex: 1;
  }
  .btn svg {
    width: 12px;
    height: 12px;
  }
  .limit {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--text-muted);
    font-size: 12px;
  }
  .editor-wrap {
    flex: 1;
    min-height: 60px;
    overflow: hidden;
  }
  .hsplit {
    height: 5px;
    margin: -2px 0;
    cursor: row-resize;
    position: relative;
    z-index: 5;
    flex-shrink: 0;
  }
  .hsplit::after {
    content: '';
    position: absolute;
    left: 0;
    right: 0;
    top: 2px;
    height: 1px;
    background: var(--border-strong);
  }
  .hsplit:hover::after {
    background: var(--accent);
    height: 2px;
  }
  .results-wrap {
    flex-shrink: 0;
    min-height: 0;
    overflow: hidden;
  }
</style>
