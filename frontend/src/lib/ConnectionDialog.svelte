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
  // A saved password exists (or, without a password store, is asked on connect).
  const hasSaved = $derived(!!(initial.hasPassword || initial.askPassword));
  const pwStore = $derived(app.passwordStore);
  // The password store may have been started since dbird opened.
  ConnectionService.PasswordStore().then((s) => (app.passwordStore = s));
  // svelte-ignore state_referenced_locally
  let useUrl = $state(!!initial.url);

  const completionHelp: Record<string, string> = {
    '': 'Up to 5,000 tables in the default schema are loaded up front; bigger schemas switch to lookups.',
    preload: 'Fastest suggestions, but reads every column of the default schema when you start typing. Too slow for schemas with hundreds of thousands of tables.',
    lookup: 'Queries table names by prefix and columns only for tables in your statement. Best for huge databases.',
    off: 'Suggests SQL keywords and functions only.',
  };

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
      if (saved.askPassword && !saved.hasPassword)
        app.toast(`Saved “${saved.name}”. No password store is available, so its password will be asked when connecting.`, 'error', 10000);
      else app.toast(`Saved “${saved.name}”`);
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

{#snippet passwordField(full: boolean)}
  <label class={[full && 'full']}>
    <span>Password</span>
    <div class="row">
      <input
        type={showPassword ? 'text' : 'password'}
        bind:value={c.password}
        disabled={c.clearPassword}
        placeholder={c.clearPassword
          ? 'Will be forgotten'
          : initial.hasPassword
            ? `Saved in ${pwStore.name}`
            : initial.askPassword
              ? 'Asked when connecting'
              : ''}
        autocomplete="off"
      />
      <button type="button" class="btn sm" onclick={() => (showPassword = !showPassword)}>{showPassword ? 'Hide' : 'Show'}</button>
    </div>
    {#if hasSaved && !c.password}
      <small>
        {c.clearPassword ? 'The saved password will be forgotten.' : 'Leave empty to keep it.'}
        <button type="button" class="link" onclick={() => (c.clearPassword = !c.clearPassword)}>{c.clearPassword ? 'Undo' : 'Forget password'}</button>
      </small>
    {/if}
    {#if !pwStore.available}
      <small class="warn">
        No password store found, so dbird won't save this password and will ask for it when connecting. Set up a password store (GNOME
        Keyring, KWallet or KeePassXC) to have it remembered securely.
      </small>
    {/if}
  </label>
{/snippet}

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
              placeholder={c.driver === 'postgres' ? 'postgres://user@host:5432/db?sslmode=disable' : 'user@tcp(host:3306)/db'}
            />
            <small>A password in the URL is moved to the password field when saving.</small>
          </label>
          {@render passwordField(true)}
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
          {@render passwordField(false)}
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

      <label class="full">
        <span>Autocomplete</span>
        <select bind:value={c.completion}>
          <option value="">Automatic — load small schemas, look up large ones as you type</option>
          <option value="preload">Load all tables and columns on connect</option>
          <option value="lookup">Look up tables and columns as you type</option>
          <option value="off">Keywords only (no metadata queries)</option>
        </select>
        <small>{completionHelp[c.completion ?? ''] ?? ''}</small>
      </label>

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
    <p class="note">
      {pwStore.available
        ? `Passwords are kept in ${pwStore.name}, not in dbird's settings file.`
        : "Passwords are never written to dbird's settings file."}
    </p>
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
  small.warn {
    color: var(--syn-number);
    line-height: 1.4;
  }
  .link {
    background: none;
    border: 0;
    padding: 0;
    color: var(--accent);
    font: inherit;
    cursor: pointer;
  }
  .link:hover {
    text-decoration: underline;
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
