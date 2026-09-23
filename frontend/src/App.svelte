<script lang="ts">
  import Sidebar from './lib/Sidebar.svelte';
  import TabBar from './lib/TabBar.svelte';
  import EditorPane from './lib/EditorPane.svelte';
  import ConnectionDialog from './lib/ConnectionDialog.svelte';
  import ConfirmDialog from './lib/ConfirmDialog.svelte';
  import { forgetEditorState } from './lib/SqlEditor.svelte';
  import { app, errorText } from './lib/state.svelte';

  let ready = $state(false);
  let initError = $state('');
  let sidebarW = $state(loadWidth());

  app
    .init()
    .then(() => (ready = true))
    .catch((e) => (initError = errorText(e)));

  function loadWidth(): number {
    try {
      return Number(localStorage.getItem('dbird.sidebarW')) || 270;
    } catch {
      return 270;
    }
  }

  function startResize(e: PointerEvent) {
    e.preventDefault();
    const startX = e.clientX;
    const startW = sidebarW;
    const move = (ev: PointerEvent) => (sidebarW = Math.max(180, Math.min(600, startW + ev.clientX - startX)));
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      try {
        localStorage.setItem('dbird.sidebarW', String(sidebarW));
      } catch {
        /* ignore */
      }
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  }

  function onkeydown(e: KeyboardEvent) {
    if (!ready || app.editing || app.confirmation) return;
    const mod = e.ctrlKey || e.metaKey;
    if (!mod) return;
    const key = e.key.toLowerCase();
    if (key === 's') {
      e.preventDefault();
      if (app.activeTab) app.saveScript(app.activeTab, e.shiftKey);
    } else if (key === 'o' && !e.shiftKey) {
      e.preventDefault();
      app.openScript();
    } else if (key === '=' || key === '+' || key === '-') {
      e.preventDefault();
      app.zoom(key === '-' ? -1 : 1);
    } else if (key === 't' && !e.shiftKey) {
      e.preventDefault();
      app.newTab();
    } else if (key === 'w' && !e.shiftKey) {
      e.preventDefault();
      if (app.activeTabId) {
        forgetEditorState(app.activeTabId);
        app.closeTab(app.activeTabId);
      }
    } else if (e.key === 'Tab' || e.key === 'PageDown' || e.key === 'PageUp') {
      e.preventDefault();
      app.cycleTab(e.key === 'PageUp' || (e.key === 'Tab' && e.shiftKey) ? -1 : 1);
    } else if (/^[1-9]$/.test(e.key) && !e.altKey) {
      const idx = Number(e.key) - 1;
      const t = e.key === '9' ? app.tabs[app.tabs.length - 1] : app.tabs[idx];
      if (t) {
        e.preventDefault();
        app.activate(t.id);
      }
    }
  }
</script>

<svelte:window {onkeydown} onbeforeunload={() => app.saveNow()} />

{#if initError}
  <div class="fatal">
    <h2>dbird failed to start</h2>
    <pre>{initError}</pre>
  </div>
{:else if ready}
  <div class="layout" style:--sidebar-w="{sidebarW}px">
    <Sidebar />
    <div class="vsplit" role="separator" aria-orientation="vertical" onpointerdown={startResize}></div>
    <main class="main">
      <TabBar />
      {#if app.activeTab}
        <EditorPane tab={app.activeTab} />
      {/if}
    </main>
  </div>
{/if}

{#if app.editing}
  <ConnectionDialog initial={app.editing} />
{/if}

{#if app.confirmation}
  <ConfirmDialog req={app.confirmation} />
{/if}

<div class="toasts" aria-live="polite">
  {#each app.toasts as t (t.id)}
    <div class={['toast', t.kind]}>{t.text}</div>
  {/each}
</div>

<style>
  .layout {
    display: grid;
    grid-template-columns: var(--sidebar-w) 0 1fr;
    /* Keep the row at the window height so the sidebar tree scrolls
       instead of growing past the bottom. */
    grid-template-rows: minmax(0, 1fr);
    height: 100%;
  }
  .vsplit {
    position: relative;
    z-index: 10;
    cursor: col-resize;
    width: 5px;
    margin-left: -3px;
    border-right: 1px solid var(--border);
  }
  .vsplit:hover {
    border-right: 2px solid var(--accent);
  }
  .main {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--editor-bg);
  }
  .main > :global(:last-child) {
    flex: 1;
    min-height: 0;
  }
  .fatal {
    padding: 32px;
  }
  .fatal pre {
    color: var(--danger);
    white-space: pre-wrap;
  }
  .toasts {
    position: fixed;
    right: 16px;
    bottom: 16px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    z-index: 200;
    pointer-events: none;
  }
  .toast {
    background: var(--panel-2);
    border: 1px solid var(--border-strong);
    border-left: 3px solid var(--accent);
    padding: 8px 12px;
    border-radius: 6px;
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.4);
    max-width: 420px;
    font-size: 12.5px;
    word-break: break-word;
    animation: slide 0.15s ease-out;
  }
  .toast.error {
    border-left-color: var(--danger);
  }
  @keyframes slide {
    from {
      transform: translateY(8px);
      opacity: 0;
    }
  }
</style>
