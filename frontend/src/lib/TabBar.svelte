<script lang="ts">
  import { app } from './state.svelte';
  import { forgetEditorState } from './SqlEditor.svelte';

  let renaming = $state('');
  let renameValue = $state('');
  let dragFrom = $state(-1);
  let dragOver = $state(-1);

  function close(id: string) {
    forgetEditorState(id);
    app.closeTab(id);
  }

  function startRename(id: string, title: string) {
    renaming = id;
    renameValue = title;
  }

  function commitRename() {
    const t = app.tabs.find((x) => x.id === renaming);
    if (t && renameValue.trim()) {
      t.title = renameValue.trim();
      app.scheduleSave();
    }
    renaming = '';
  }

  function focusSelect(el: HTMLInputElement) {
    el.focus();
    el.select();
  }
</script>

<div class="tabbar" role="tablist">
  {#each app.tabs as t, i (t.id)}
    {@const conn = app.connection(t.connectionId)}
    {@const running = app.rt(t.id).running}
    <div
      class={['tab', t.id === app.activeTabId && 'active', dragOver === i && dragFrom !== i && 'dragover']}
      role="tab"
      tabindex="0"
      aria-selected={t.id === app.activeTabId}
      style:--conn-color={conn?.color || undefined}
      draggable="true"
      title={[t.filePath || t.title, conn?.name].filter(Boolean).join(' — ')}
      onclick={() => app.activate(t.id)}
      onauxclick={(e) => e.button === 1 && close(t.id)}
      ondblclick={() => startRename(t.id, t.title)}
      onkeydown={(e) => e.key === 'Enter' && app.activate(t.id)}
      ondragstart={(e) => {
        dragFrom = i;
        e.dataTransfer?.setData('text/plain', t.id);
      }}
      ondragover={(e) => {
        e.preventDefault();
        dragOver = i;
      }}
      ondragleave={() => (dragOver = -1)}
      ondrop={(e) => {
        e.preventDefault();
        if (dragFrom >= 0) app.moveTab(dragFrom, i);
        dragFrom = dragOver = -1;
      }}
      ondragend={() => (dragFrom = dragOver = -1)}
    >
      {#if running}
        <span class="spin"></span>
      {:else}
        <svg class="ico" viewBox="0 0 16 16" aria-hidden="true"><path d="M4 2h5l3 3v9H4z M9 2v3h3" stroke="currentColor" stroke-width="1.2" fill="none" /></svg>
      {/if}
      {#if renaming === t.id}
        <input
          class="rename"
          bind:value={renameValue}
          {@attach focusSelect}
          onblur={commitRename}
          onkeydown={(e) => {
            e.stopPropagation();
            if (e.key === 'Enter') commitRename();
            if (e.key === 'Escape') renaming = '';
          }}
          onclick={(e) => e.stopPropagation()}
        />
      {:else}
        <span class="title">{t.title}</span>
        {#if t.filePath && t.dirty}<span class="dirty" title="Unsaved changes">•</span>{/if}
      {/if}
      <button
        class="close"
        aria-label="Close tab"
        onclick={(e) => {
          e.stopPropagation();
          close(t.id);
        }}>×</button
      >
    </div>
  {/each}
  <button class="new" title="New SQL editor (Ctrl+T)" aria-label="New tab" onclick={() => app.newTab()}>+</button>
</div>

<style>
  .tabbar {
    display: flex;
    align-items: stretch;
    height: 36px;
    background: var(--sidebar-bg);
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    overflow-y: hidden;
    flex-shrink: 0;
    scrollbar-width: thin;
  }
  .tab {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 6px 0 10px;
    min-width: 110px;
    max-width: 220px;
    border-right: 1px solid var(--border);
    color: var(--text-muted);
    cursor: default;
    font-size: 12.5px;
    position: relative;
    user-select: none;
    flex-shrink: 0;
    box-shadow: inset 0 2px 0 var(--conn-color, transparent);
  }
  .tab:hover {
    background: var(--hover);
    color: var(--text);
  }
  .tab.active {
    background: var(--editor-bg);
    color: var(--text);
  }
  .tab.active::after {
    content: '';
    position: absolute;
    left: 0;
    right: 0;
    bottom: -1px;
    height: 1px;
    background: var(--editor-bg);
  }
  .tab.active {
    box-shadow: inset 0 2px 0 var(--conn-color, var(--accent));
  }
  .tab.dragover {
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .title {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dirty {
    color: var(--text-muted);
    font-size: 16px;
    line-height: 1;
    margin-left: -3px;
  }
  .rename {
    flex: 1;
    min-width: 0;
    padding: 1px 4px;
    font-size: 12px;
  }
  .ico {
    width: 12px;
    height: 12px;
    flex-shrink: 0;
    opacity: 0.8;
  }
  .close {
    border: none;
    background: none;
    color: var(--text-faint);
    width: 18px;
    height: 18px;
    border-radius: 4px;
    font-size: 15px;
    line-height: 1;
    padding: 0;
    cursor: pointer;
    visibility: hidden;
  }
  .tab:hover .close,
  .tab.active .close {
    visibility: visible;
  }
  .close:hover {
    background: var(--hover-strong);
    color: var(--text);
  }
  .new {
    border: none;
    background: none;
    color: var(--text-muted);
    font-size: 18px;
    width: 34px;
    cursor: pointer;
    flex-shrink: 0;
  }
  .new:hover {
    color: var(--text);
    background: var(--hover);
  }
  .spin {
    width: 10px;
    height: 10px;
    border: 1.5px solid var(--border-strong);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
    flex-shrink: 0;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
