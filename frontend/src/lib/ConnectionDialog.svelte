<script lang="ts">
  import { ConnectionService } from '../../bindings/dbird';
  import { app, defaultPorts, errorText, type Connection } from './state.svelte';

  let { initial }: { initial: Connection } = $props();

  // Local editable copy; the dialog is re-created for every edit.
  // svelte-ignore state_referenced_locally
  let c = $state<Connection>({ ...initial });
  let testing = $state(false);
  let saving = $state(false);
  let testMsg = $state<{ ok: boolean; text: string } | null>(null);
  let showPassword = $state(false);
  // svelte-ignore state_referenced_locally
  let useUrl = $state(!!initial.url);

  const colors = ['', '#4caf50', '#2196f3', '#ff9800', '#e91e63', '#9c27b0', '#f44336', '#00bcd4'];
  const isNew = $derived(!initial.id);
  const isSqlite = $derived(c.driver === 'sqlite');

  function setDriver(d: string) {
    const wasDefault = !c.port || c.port === defaultPorts[c.driver];
    c.driver = d;
    if (wasDefault) c.port = defaultPorts[d] ?? 0;
    if (d === 'mysql' && !c.user) c.user = 'root';
    if (d === 'postgres' && !c.sslMode) c.sslMode = 'prefer';
    testMsg = null;
  }

  function payload(): Connection {
    const out = { ...$state.snapshot(c) } as Connection;
    if (!useUrl) out.url = '';
    out.port = Number(out.port) || 0;
    if (!out.name.trim()) {
      out.name = isSqlite
        ? out.database.split(/[\\/]/).pop() || 'sqlite'
        : `${out.database || out.driver}@${out.host || 'localhost'}`;
    }
    return out;
  }

  async function test() {
    testing = true;
    testMsg = null;
    try {
      const v = await ConnectionService.Test(payload());
      testMsg = { ok: true, text: `Connected${v ? ` — server ${v}` : ''}` };
    } catch (e) {
      testMsg = { ok: false, text: errorText(e) };
    } finally {
      testing = false;
    }
  }

  async function save(e: SubmitEvent) {
    e.preventDefault();
    saving = true;
    try {
      const wasNew = isNew;
      const saved = await app.saveConnection(payload());
      if (wasNew && app.activeTab && !app.activeTab.connectionId) {
        app.activeTab.connectionId = saved.id;
        app.scheduleSave();
      }
      app.toast(`Saved “${saved.name}”`);
      app.editing = null;
    } catch (err) {
      testMsg = { ok: false, text: errorText(err) };
    } finally {
      saving = false;
    }
  }

  async function browse() {
    try {
      const p = await ConnectionService.PickSQLiteFile();
      if (p) c.database = p;
    } catch (e) {
      testMsg = { ok: false, text: errorText(e) };
    }
  }

  function close() {
    app.editing = null;
  }
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && close()} />

<div class="backdrop" role="presentation" onmousedown={(e) => e.target === e.currentTarget && close()}>
  <form class="dialog" onsubmit={save} aria-label="Connection settings">
    <header>
      <h2>{isNew ? 'New connection' : `Edit ${initial.name}`}</h2>
      <button type="button" class="icon-btn" onclick={close} aria-label="Close">✕</button>
    </header>

    <div class="drivers" role="radiogroup" aria-label="Database type">
      {#each [['postgres', 'PostgreSQL'], ['mysql', 'MySQL / MariaDB'], ['sqlite', 'SQLite']] as [id, label] (id)}
        <button type="button" role="radio" aria-checked={c.driver === id} class={['driver', c.driver === id && 'on']} onclick={() => setDriver(id)}>
          <span class={['dbadge', id]}></span>{label}
        </button>
      {/each}
    </div>

    <div class="grid">
      <label class="full">
        <span>Name</span>
        <input bind:value={c.name} placeholder="My database" autocomplete="off" />
      </label>

      {#if isSqlite}
        <label class="full">
          <span>Database file</span>
          <div class="row">
            <input bind:value={c.database} placeholder="/path/to/database.db" required autocomplete="off" />
            <button type="button" class="btn" onclick={browse}>Browse…</button>
          </div>
          <small>The file is created if it doesn't exist.</small>
        </label>
      {:else}
        <label class="full check">
          <input type="checkbox" bind:checked={useUrl} />
          <span>Use connection URL / DSN</span>
        </label>

        {#if useUrl}
          <label class="full">
            <span>URL</span>
            <input
              bind:value={c.url}
              required
              autocomplete="off"
              placeholder={c.driver === 'postgres' ? 'postgres://user:pass@host:5432/db?sslmode=disable' : 'user:pass@tcp(host:3306)/db'}
            />
          </label>
        {:else}
          <label class="host">
            <span>Host</span>
            <input bind:value={c.host} placeholder="localhost" autocomplete="off" />
          </label>
          <label class="port">
            <span>Port</span>
            <input type="number" min="0" max="65535" bind:value={c.port} />
          </label>
          <label class="full">
            <span>Database</span>
            <input bind:value={c.database} placeholder={c.driver === 'postgres' ? 'postgres' : '(optional)'} autocomplete="off" />
          </label>
          <label>
            <span>User</span>
            <input bind:value={c.user} autocomplete="off" />
          </label>
          <label>
            <span>Password</span>
            <div class="row">
              <input type={showPassword ? 'text' : 'password'} bind:value={c.password} autocomplete="off" />
              <button type="button" class="btn sm" onclick={() => (showPassword = !showPassword)}>{showPassword ? 'Hide' : 'Show'}</button>
            </div>
          </label>
          {#if c.driver === 'postgres'}
            <label>
              <span>SSL mode</span>
              <select bind:value={c.sslMode}>
                {#each ['disable', 'allow', 'prefer', 'require', 'verify-ca', 'verify-full'] as m (m)}
                  <option value={m}>{m}</option>
                {/each}
              </select>
            </label>
          {/if}
        {/if}
      {/if}

      <div class="full">
        <span class="lbl">Color</span>
        <div class="colors">
          {#each colors as col (col)}
            <button
              type="button"
              class={['swatch', c.color === col && 'on', !col && 'none']}
              style:--sw={col || 'transparent'}
              aria-label={col || 'No color'}
              onclick={() => (c.color = col)}
            ></button>
          {/each}
        </div>
      </div>
    </div>

    {#if testMsg}
      <div class={['msg', testMsg.ok ? 'ok' : 'bad']}>{testMsg.text}</div>
    {/if}

    <footer>
      <button type="button" class="btn" onclick={test} disabled={testing}>{testing ? 'Testing…' : 'Test connection'}</button>
      <span class="spacer"></span>
      <button type="button" class="btn" onclick={close}>Cancel</button>
      <button type="submit" class="btn primary" disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
    </footer>
    <p class="note">Credentials are stored locally in your config directory (file readable only by you).</p>
  </form>
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
    width: min(560px, 100%);
    max-height: calc(100vh - 32px);
    overflow: auto;
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
  .drivers {
    display: flex;
    gap: 6px;
    margin-bottom: 14px;
    flex-wrap: wrap;
  }
  .driver {
    display: flex;
    align-items: center;
    gap: 7px;
    flex: 1;
    padding: 8px 10px;
    border: 1px solid var(--border-strong);
    border-radius: 7px;
    background: var(--input-bg);
    color: var(--text-muted);
    cursor: pointer;
    font-size: 12.5px;
    white-space: nowrap;
  }
  .driver.on {
    border-color: var(--accent);
    color: var(--text);
    background: var(--accent-bg);
  }
  .dbadge {
    width: 9px;
    height: 9px;
    border-radius: 2px;
  }
  .dbadge.postgres {
    background: #336791;
  }
  .dbadge.mysql {
    background: #c17a00;
  }
  .dbadge.sqlite {
    background: #3f7d6e;
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px 12px;
  }
  label,
  .full {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  label > span,
  .lbl {
    font-size: 11.5px;
    color: var(--text-muted);
  }
  .full {
    grid-column: 1 / -1;
  }
  .grid:has(.host) .host {
    grid-column: 1;
  }
  .check {
    flex-direction: row;
    align-items: center;
    gap: 8px;
  }
  .check > span {
    font-size: 12.5px;
    color: var(--text);
  }
  .row {
    display: flex;
    gap: 6px;
  }
  .row input {
    flex: 1;
    min-width: 0;
  }
  small {
    color: var(--text-faint);
    font-size: 11px;
  }
  .colors {
    display: flex;
    gap: 6px;
  }
  .swatch {
    width: 20px;
    height: 20px;
    border-radius: 50%;
    border: 2px solid transparent;
    background: var(--sw);
    cursor: pointer;
    outline: 1px solid var(--border-strong);
  }
  .swatch.none {
    background: repeating-linear-gradient(45deg, transparent 0 3px, var(--border-strong) 3px 4px);
  }
  .swatch.on {
    border-color: var(--panel);
    outline: 2px solid var(--text);
  }
  .msg {
    margin-top: 12px;
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .msg.ok {
    background: var(--success-bg);
    color: var(--success);
  }
  .msg.bad {
    background: var(--danger-bg);
    color: var(--danger);
  }
  footer {
    display: flex;
    gap: 8px;
    margin-top: 16px;
    align-items: center;
  }
  .spacer {
    flex: 1;
  }
  .note {
    margin: 10px 0 0;
    font-size: 10.5px;
    color: var(--text-faint);
  }
</style>
