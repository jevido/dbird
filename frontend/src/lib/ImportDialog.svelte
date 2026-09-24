<script lang="ts">
  import { ConnectionService } from '../../bindings/dbird';
  import type { Candidate } from '../../bindings/dbird/internal/dbeaver/models';
  import { app, errorText } from './state.svelte';

  let dir = $state('');
  let candidates = $state.raw<Candidate[]>([]);
  let selected = $state<Record<string, boolean>>({});
  let loading = $state(true);
  let importing = $state(false);
  let error = $state('');

  const importable = $derived(candidates.filter((c) => !c.problem));
  const count = $derived(importable.filter((c) => selected[c.key]).length);
  const allOn = $derived(importable.length > 0 && count === importable.length);

  async function scan(from: string) {
    loading = true;
    error = '';
    try {
      const res = await ConnectionService.ScanDBeaver(from);
      dir = res.dir;
      candidates = res.candidates ?? [];
      // Preselect what can be imported and isn't in dbird already.
      selected = Object.fromEntries(candidates.map((c) => [c.key, !c.problem && !c.existing]));
    } catch (e) {
      error = errorText(e);
      candidates = [];
    } finally {
      loading = false;
    }
  }

  async function choose() {
    try {
      const p = await ConnectionService.PickDBeaverFolder();
      if (p) await scan(p);
    } catch (e) {
      error = errorText(e);
    }
  }

  function toggleAll() {
    const on = !allOn;
    selected = Object.fromEntries(candidates.map((c) => [c.key, on && !c.problem]));
  }

  async function doImport() {
    importing = true;
    error = '';
    try {
      const conns = importable.filter((c) => selected[c.key]).map((c) => c.connection);
      const n = await ConnectionService.Import(conns);
      await app.reloadConnections();
      app.toast(`Imported ${n} connection${n === 1 ? '' : 's'} from DBeaver`);
      app.importing = false;
    } catch (e) {
      error = errorText(e);
    } finally {
      importing = false;
    }
  }

  function target(c: Candidate) {
    const x = c.connection;
    if (x.driver === 'sqlite') return x.database;
    const host = `${x.host || 'localhost'}${x.port ? `:${x.port}` : ''}`;
    return `${x.user ? `${x.user}@` : ''}${host}${x.database ? `/${x.database}` : ''}`;
  }

  function close() {
    app.importing = false;
  }

  scan('');

  const driverLabel: Record<string, string> = { postgres: 'PG', mysql: 'My', sqlite: 'SL' };
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && close()} />

<div class="backdrop" role="presentation" onmousedown={(e) => e.target === e.currentTarget && close()}>
  <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="import-title">
    <header>
      <h2 id="import-title">Import from DBeaver</h2>
      <button type="button" class="icon-btn" onclick={close} aria-label="Close">✕</button>
    </header>

    <div class="source">
      <span class="path" title={dir}>{dir || 'No DBeaver workspace found'}</span>
      <button class="btn" onclick={choose} disabled={loading || importing}>Choose folder…</button>
    </div>

    {#if loading}
      <p class="muted">Reading DBeaver connections…</p>
    {:else if !dir && !error}
      <p class="muted">
        DBeaver keeps its connections in a <code>DBeaverData/workspace6</code> folder. Choose it, or one of the project folders
        inside it.
      </p>
    {:else if candidates.length > 0}
      <div class="list" role="group" aria-label="Connections">
        <label class="row all">
          <input type="checkbox" checked={allOn} onchange={toggleAll} disabled={importable.length === 0} />
          <span>{candidates.length} connection{candidates.length === 1 ? '' : 's'} found</span>
        </label>
        {#each candidates as c (c.key)}
          <label class={['row', c.problem && 'off']} style:--conn-color={c.connection.color || 'transparent'}>
            <input type="checkbox" bind:checked={selected[c.key]} disabled={!!c.problem} />
            <span class="info">
              <span class="name">
                {#if !c.problem}<span class={['badge', c.connection.driver]}>{driverLabel[c.connection.driver]}</span>{/if}
                {c.connection.name}
              </span>
              {#if c.problem}
                <span class="note bad">{c.problem}</span>
              {:else}
                <span class="target">{target(c)}</span>
                {#if c.existing}<span class="note">Already in dbird as “{c.existing}”</span>{/if}
                {#each c.warnings ?? [] as w (w)}<span class="note warn">{w}</span>{/each}
              {/if}
            </span>
          </label>
        {/each}
      </div>
    {/if}

    {#if error}<p class="error">{error}</p>{/if}

    <footer>
      <small>Passwords are stored in dbird's settings file like any other connection.</small>
      <button class="btn" onclick={close}>Cancel</button>
      <button class="btn primary" onclick={doImport} disabled={count === 0 || importing}>
        {importing ? 'Importing…' : `Import ${count || ''}`.trim()}
      </button>
    </footer>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: grid;
    place-items: center;
    z-index: 50;
    padding: 16px;
  }
  .dialog {
    width: min(620px, 100%);
    max-height: calc(100vh - 32px);
    display: flex;
    flex-direction: column;
    background: var(--panel);
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
    padding: 16px 18px 12px;
  }
  header {
    display: flex;
    align-items: center;
    margin-bottom: 12px;
  }
  h2 {
    flex: 1;
    margin: 0;
    font-size: 15px;
    font-weight: 600;
  }
  .source {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 10px;
  }
  .path {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text-muted);
    direction: rtl;
    text-align: left;
  }
  .muted {
    color: var(--text-muted);
    line-height: 1.45;
  }
  code {
    font-family: var(--mono);
    font-size: 12px;
  }
  .list {
    overflow: auto;
    min-height: 0;
    border: 1px solid var(--border);
    border-radius: 7px;
    background: var(--input-bg);
  }
  .row {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 8px 10px;
    border-bottom: 1px solid var(--border);
    border-left: 3px solid var(--conn-color, transparent);
    cursor: pointer;
  }
  .row:last-child {
    border-bottom: 0;
  }
  .row.all {
    position: sticky;
    top: 0;
    background: var(--panel);
    color: var(--text-muted);
    font-size: 12px;
    align-items: center;
  }
  .row.off {
    cursor: default;
    opacity: 0.6;
  }
  .row input {
    margin-top: 2px;
  }
  .info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .name {
    display: flex;
    align-items: center;
    gap: 6px;
    font-weight: 500;
  }
  .target {
    font-family: var(--mono);
    font-size: 11.5px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .note {
    font-size: 11.5px;
    color: var(--text-faint);
  }
  .note.warn {
    color: var(--syn-number);
  }
  .note.bad {
    color: var(--danger);
  }
  .badge {
    font-size: 9.5px;
    font-weight: 700;
    padding: 1px 4px;
    border-radius: 3px;
    color: #fff;
  }
  .badge.postgres {
    background: #336791;
  }
  .badge.mysql {
    background: #c17a00;
  }
  .badge.sqlite {
    background: #3f7d6e;
  }
  .error {
    color: var(--danger);
    margin: 10px 0 0;
  }
  footer {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
  }
  footer small {
    flex: 1;
    color: var(--text-faint);
  }
</style>
