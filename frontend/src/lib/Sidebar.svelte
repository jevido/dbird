<script lang="ts">
  import { ConnectionService } from '../../bindings/dbird';
  import type { ColumnInfo, TableInfo } from '../../bindings/dbird/internal/dbx/models';
  import { app, emptyConnection, errorText, type Connection } from './state.svelte';
  import { quoteIdent } from './sqlutil';
  import UpdateBar from './UpdateBar.svelte';
  import logo from '../assets/logo.png';

  type NodeState = { loading?: boolean; error?: string };

  let expanded = $state<Record<string, boolean>>({});
  let nodeState = $state<Record<string, NodeState>>({});
  let schemas = $state.raw<Record<string, string[]>>({});
  // Tables per schema, loaded a page at a time. `filter` is the name filter
  // the server applied ("" = none); a list with every table loaded is
  // filtered here instead.
  interface TableList {
    items: TableInfo[];
    total: number;
    filter: string;
  }
  const PAGE = 500;
  let tables = $state.raw<Record<string, TableList>>({});
  const complete = (l: TableList | undefined) => !!l && l.filter === '' && l.items.length >= l.total;
  let columns = $state.raw<Record<string, ColumnInfo[]>>({});
  let filter = $state('');

  let menu = $state<{ x: number; y: number; items: MenuItem[] } | null>(null);
  let confirmDelete = $state('');

  type MenuItem = { label: string; action: () => void; danger?: boolean; disabled?: boolean } | 'sep';

  const k = {
    conn: (c: string) => c,
    schema: (c: string, s: string) => `${c}\u0000${s}`,
    table: (c: string, s: string, t: string) => `${c}\u0000${s}\u0000${t}`,
  };

  const needle = $derived(filter.trim().toLowerCase());

  // With a filter, show connections whose name matches or that have matching loaded tables.
  const visibleConnections = $derived(
    needle
      ? app.connections.filter(
          (c) =>
            c.name.toLowerCase().includes(needle) ||
            Object.entries(tables).some(
              ([key, list]) =>
                key.startsWith(c.id + '\u0000') &&
                (complete(list) ? list.items.some((t) => t.name.toLowerCase().includes(needle)) : list.items.length > 0),
            ),
        )
      : app.connections,
  );

  function visibleTables(c: Connection, key: string): TableInfo[] {
    const list = tables[key];
    if (!list) return [];
    if (!complete(list) || !needle || c.name.toLowerCase().includes(needle)) return list.items;
    return list.items.filter((t) => t.name.toLowerCase().includes(needle));
  }

  // Big schemas aren't fully loaded, so the filter box asks the server.
  $effect(() => {
    const f = needle;
    const stale = Object.entries(tables).filter(([, l]) => !complete(l) && l.filter !== f);
    if (stale.length === 0) return;
    const t = setTimeout(() => {
      for (const [key] of stale) {
        const [connId, schema] = key.split('\u0000');
        const c = app.connection(connId);
        if (c) loadTables(c, schema);
      }
    }, 250);
    return () => clearTimeout(t);
  });

  async function load<T>(key: string, fn: () => Promise<T>, set: (v: T) => void) {
    nodeState[key] = { loading: true };
    try {
      set(await fn());
      nodeState[key] = {};
    } catch (e) {
      nodeState[key] = { error: errorText(e) };
    }
  }

  function singleSchema(c: Connection) {
    return c.driver === 'sqlite';
  }

  async function loadSchemas(c: Connection) {
    await load(k.conn(c.id), () => ConnectionService.Schemas(c.id), (v) => (schemas = { ...schemas, [c.id]: v ?? [] }));
    if (singleSchema(c)) {
      const s = schemas[c.id]?.[0];
      if (s) loadTables(c, s);
    }
  }

  // Loads the first page of schema's tables, or with more=true the next one.
  function loadTables(c: Connection, schema: string, more = false) {
    const key = k.schema(c.id, schema);
    const prev = tables[key];
    const filter = more ? (prev?.filter ?? '') : complete(prev) ? '' : needle;
    const after = more && prev ? (prev.items.at(-1)?.name ?? '') : '';
    return load(
      more ? key + '\u0000more' : key,
      () => ConnectionService.TablesPage(c.id, schema, filter, after, PAGE),
      (page) => {
        const items = page.tables ?? [];
        tables = {
          ...tables,
          [key]: more && prev ? { ...prev, items: [...prev.items, ...items] } : { items, total: page.total, filter },
        };
      },
    );
  }

  function loadColumns(c: Connection, schema: string, table: string) {
    const key = k.table(c.id, schema, table);
    return load(key, () => ConnectionService.Columns(c.id, schema, table), (v) => (columns = { ...columns, [key]: v ?? [] }));
  }

  async function toggleConn(c: Connection) {
    const key = k.conn(c.id);
    // A tab without a connection adopts the one the user just picked.
    if (app.activeTab && !app.activeTab.connectionId) {
      app.activeTab.connectionId = c.id;
      app.scheduleSave();
    }
    if (expanded[key]) {
      expanded[key] = false;
      return;
    }
    if (!(await app.connect(c.id))) return;
    expanded[key] = true;
    if (!schemas[c.id]) loadSchemas(c);
  }

  function toggleSchema(c: Connection, s: string) {
    const key = k.schema(c.id, s);
    expanded[key] = !expanded[key];
    if (expanded[key] && !tables[key]) loadTables(c, s);
  }

  function toggleTable(c: Connection, s: string, t: string) {
    const key = k.table(c.id, s, t);
    expanded[key] = !expanded[key];
    if (expanded[key] && !columns[key]) loadColumns(c, s, t);
  }

  function refresh(c: Connection) {
    const prefix = c.id;
    const strip = <T,>(m: Record<string, T>) =>
      Object.fromEntries(Object.entries(m).filter(([key]) => key !== prefix && !key.startsWith(prefix + '\u0000')));
    schemas = strip(schemas);
    tables = strip(tables);
    columns = strip(columns);
    if (app.connected[c.id]) {
      expanded[k.conn(c.id)] = true;
      loadSchemas(c).then(() => {
        for (const s of schemas[c.id] ?? []) if (expanded[k.schema(c.id, s)]) loadTables(c, s);
      });
    }
    app.loadCompletions(c.id);
  }

  async function disconnect(c: Connection) {
    await app.disconnect(c.id);
    expanded[k.conn(c.id)] = false;
  }

  function qualified(c: Connection, schema: string, table: string): string {
    const t = quoteIdent(c.driver, table);
    if (c.driver === 'sqlite' && schema === 'main') return t;
    return `${quoteIdent(c.driver, schema)}.${t}`;
  }

  function openTable(c: Connection, schema: string, table: string) {
    const sql = `SELECT * FROM ${qualified(c, schema, table)}\nLIMIT 200;\n`;
    const tab = app.newTab(c.id, sql, table);
    app.run(tab, [sql.trim().replace(/;$/, '')]);
  }

  function showMenu(e: MouseEvent, items: MenuItem[]) {
    e.preventDefault();
    e.stopPropagation();
    confirmDelete = '';
    const x = Math.min(e.clientX, window.innerWidth - 200);
    const y = Math.min(e.clientY, window.innerHeight - items.length * 28 - 12);
    menu = { x, y, items };
  }

  function connMenu(e: MouseEvent, c: Connection) {
    const on = app.connected[c.id];
    showMenu(e, [
      { label: 'New SQL script', action: () => app.newTab(c.id) },
      'sep',
      on ? { label: 'Disconnect', action: () => disconnect(c) } : { label: 'Connect', action: () => toggleConn(c) },
      { label: 'Refresh', action: () => refresh(c), disabled: !on },
      'sep',
      { label: 'Edit connection…', action: () => (app.editing = { ...c }) },
      { label: 'Duplicate', action: () => (app.editing = { ...c, id: '', name: c.name + ' copy', copyFrom: c.id }) },
      { label: 'Delete…', danger: true, action: () => askDelete(c) },
    ]);
  }

  function tableMenu(e: MouseEvent, c: Connection, schema: string, t: TableInfo) {
    showMenu(e, [
      { label: 'Open data', action: () => openTable(c, schema, t.name) },
      {
        label: 'Generate SELECT in new tab',
        action: () => app.newTab(c.id, `SELECT *\nFROM ${qualified(c, schema, t.name)}\nWHERE 1 = 1\nLIMIT 100;\n`, t.name),
      },
      { label: 'Count rows', action: () => {
        const sql = `SELECT COUNT(*) FROM ${qualified(c, schema, t.name)}`;
        app.run(app.newTab(c.id, sql + ';\n', `count ${t.name}`), [sql]);
      } },
      'sep',
      { label: 'Copy name', action: () => navigator.clipboard?.writeText(qualified(c, schema, t.name)) },
    ]);
  }

  function askDelete(c: Connection) {
    confirmDelete = c.id;
  }

  async function doDelete(c: Connection) {
    confirmDelete = '';
    try {
      await app.deleteConnection(c.id);
    } catch (e) {
      app.toast(errorText(e), 'error');
    }
  }

  const driverLabel: Record<string, string> = { postgres: 'PG', mysql: 'My', sqlite: 'SL' };
</script>

<svelte:window onclick={() => (menu = null)} onkeydown={(e) => e.key === 'Escape' && (menu = null)} />

<aside class="sidebar">
  <div class="head">
    <img class="logo" src={logo} alt="" />
    <span class="brand">dbird</span>
    <span class="title">Connections</span>
    <button class="icon-btn" title="Import from DBeaver" onclick={() => (app.importing = true)}>
      <svg viewBox="0 0 16 16"><path d="M8 2v8M4.5 6.5 8 10l3.5-3.5M3 12.5h10" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round" /></svg>
    </button>
    <button class="icon-btn" title="New connection" onclick={() => (app.editing = emptyConnection())}>
      <svg viewBox="0 0 16 16"><path d="M8 3v10M3 8h10" stroke="currentColor" stroke-width="1.6" fill="none" /></svg>
    </button>
  </div>
  {#if app.connections.length > 0}
    <div class="filter">
      <input
        type="search"
        placeholder="Filter connections & tables"
        bind:value={filter}
        onkeydown={(e) => e.key === 'Escape' && (filter = '')}
      />
    </div>
  {/if}

  <div class="tree" role="tree">
    {#if app.connections.length === 0}
      <div class="empty">
        <p>No connections yet.</p>
        <button class="btn primary" onclick={() => (app.editing = emptyConnection())}>New connection</button>
        <button class="btn" onclick={() => (app.importing = true)}>Import from DBeaver</button>
      </div>
    {/if}

    {#each visibleConnections as c (c.id)}
      {@const ckey = k.conn(c.id)}
      {@const st = nodeState[ckey]}
      <div
        class={['node', 'conn', app.activeTab?.connectionId === c.id && 'current']}
        role="treeitem"
        aria-selected={app.activeTab?.connectionId === c.id}
        aria-expanded={!!expanded[ckey]}
        tabindex="0"
        style:--conn-color={c.color || 'var(--text-faint)'}
        onclick={() => toggleConn(c)}
        ondblclick={async () => (await app.connect(c.id)) && app.newTab(c.id)}
        onkeydown={(e) => e.key === 'Enter' && toggleConn(c)}
        oncontextmenu={(e) => connMenu(e, c)}
      >
        <span class={['caret', expanded[ckey] && 'open']}><svg viewBox="0 0 16 16"><path d="M6 4l4 4-4 4" stroke="currentColor" stroke-width="1.6" fill="none" stroke-linecap="round" stroke-linejoin="round" /></svg></span>
        <span class={['badge', c.driver]}>{driverLabel[c.driver] ?? '?'}</span>
        <span class="label">{c.name}</span>
        {#if app.connecting[c.id] || st?.loading}
          <span class="spin"></span>
        {:else if app.connected[c.id]}
          <span class="live" title="Connected"></span>
        {/if}
        <span class="actions">
          <button class="icon-btn sm" title="New SQL script" onclick={(e) => (e.stopPropagation(), app.newTab(c.id))}>
            <svg viewBox="0 0 16 16"><path d="M4 2h5l3 3v9H4z M9 2v3h3" stroke="currentColor" stroke-width="1.3" fill="none" /></svg>
          </button>
          <button class="icon-btn sm" title="More" onclick={(e) => connMenu(e, c)}>
            <svg viewBox="0 0 16 16"><circle cx="4" cy="8" r="1.2" fill="currentColor" /><circle cx="8" cy="8" r="1.2" fill="currentColor" /><circle cx="12" cy="8" r="1.2" fill="currentColor" /></svg>
          </button>
        </span>
      </div>

      {#if confirmDelete === c.id}
        <div class="confirm">
          Delete “{c.name}”?
          <button class="btn danger sm" onclick={() => doDelete(c)}>Delete</button>
          <button class="btn sm" onclick={() => (confirmDelete = '')}>Cancel</button>
        </div>
      {/if}

      {#if expanded[ckey] && app.connected[c.id]}
        {#if st?.error}
          <div class="node err" style:--depth="1">{st.error}</div>
        {/if}
        {#each schemas[c.id] ?? [] as s (s)}
          {@const skey = k.schema(c.id, s)}
          {@const sst = nodeState[skey]}
          {@const flat = singleSchema(c)}
          {#if !flat}
            <div
              class="node"
              role="treeitem"
              aria-selected="false"
              aria-expanded={!!expanded[skey]}
              tabindex="0"
              style:--depth="1"
              onclick={() => toggleSchema(c, s)}
              onkeydown={(e) => e.key === 'Enter' && toggleSchema(c, s)}
            >
              <span class={['caret', expanded[skey] && 'open']}><svg viewBox="0 0 16 16"><path d="M6 4l4 4-4 4" stroke="currentColor" stroke-width="1.6" fill="none" stroke-linecap="round" stroke-linejoin="round" /></svg></span>
              <svg class="ico" viewBox="0 0 16 16"><ellipse cx="8" cy="4" rx="5" ry="2" stroke="currentColor" fill="none" /><path d="M3 4v8c0 1.1 2.2 2 5 2s5-.9 5-2V4" stroke="currentColor" fill="none" /></svg>
              <span class="label">{s}</span>
              {#if sst?.loading}<span class="spin"></span>{/if}
              {#if tables[skey]}<span class="count">{tables[skey].total.toLocaleString()}</span>{/if}
            </div>
          {/if}
          {#if flat || expanded[skey]}
            {@const depth = flat ? 1 : 2}
            {#if sst?.error}
              <div class="node err" style:--depth={depth}>{sst.error}</div>
            {/if}
            {#if tables[skey]?.items.length === 0}
              <div class="node muted" style:--depth={depth}>{tables[skey].filter ? 'No matching tables' : 'No tables'}</div>
            {/if}
            {#each visibleTables(c, skey) as t (t.name)}
              {@const tkey = k.table(c.id, s, t.name)}
              {@const tst = nodeState[tkey]}
              <div
                class="node"
                role="treeitem"
                aria-selected="false"
                aria-expanded={!!expanded[tkey]}
                tabindex="0"
                style:--depth={depth}
                title="Double-click to open data"
                onclick={() => toggleTable(c, s, t.name)}
                ondblclick={() => openTable(c, s, t.name)}
                onkeydown={(e) => e.key === 'Enter' && openTable(c, s, t.name)}
                oncontextmenu={(e) => tableMenu(e, c, s, t)}
              >
                <span class={['caret', expanded[tkey] && 'open']}><svg viewBox="0 0 16 16"><path d="M6 4l4 4-4 4" stroke="currentColor" stroke-width="1.6" fill="none" stroke-linecap="round" stroke-linejoin="round" /></svg></span>
                {#if t.kind === 'view'}
                  <svg class="ico view" viewBox="0 0 16 16"><path d="M1.5 8s2.5-4.5 6.5-4.5S14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z" stroke="currentColor" fill="none" /><circle cx="8" cy="8" r="2" stroke="currentColor" fill="none" /></svg>
                {:else}
                  <svg class="ico table" viewBox="0 0 16 16"><rect x="2" y="3" width="12" height="10" rx="1" stroke="currentColor" fill="none" /><path d="M2 6.5h12M6 6.5V13" stroke="currentColor" /></svg>
                {/if}
                <span class="label">{t.name}</span>
                {#if tst?.loading}<span class="spin"></span>{/if}
              </div>
              {#if expanded[tkey]}
                {#if tst?.error}
                  <div class="node err" style:--depth={depth + 1}>{tst.error}</div>
                {/if}
                {#each columns[tkey] ?? [] as col (col.name)}
                  <div class="node leaf" style:--depth={depth + 1} title="{col.name} {col.type}{col.nullable ? '' : ' NOT NULL'}">
                    <span class="caret"></span>
                    <span class={['colicon', col.primaryKey && 'pk']}>{col.primaryKey ? '🔑' : '•'}</span>
                    <span class="label">{col.name}</span>
                    <span class="type">{col.type}</span>
                  </div>
                {/each}
              {/if}
            {/each}
            {#if tables[skey] && tables[skey].items.length < tables[skey].total}
              {@const list = tables[skey]}
              {@const busy = nodeState[skey + '\u0000more']?.loading}
              <button class="node more" style:--depth={depth} disabled={busy} onclick={() => loadTables(c, s, true)}>
                <span class="caret"></span>
                {busy ? 'Loading…' : `Show ${Math.min(PAGE, list.total - list.items.length).toLocaleString()} more`}
                <span class="count">{list.items.length.toLocaleString()} of {list.total.toLocaleString()}{list.filter ? ' matching' : ''}</span>
              </button>
              {#if !list.filter}
                <div class="node muted hint" style:--depth={depth}><span class="caret"></span>Type in the filter box to search all of them</div>
              {/if}
            {/if}
          {/if}
        {/each}
      {/if}
    {/each}
  </div>
  <UpdateBar />
</aside>

{#if menu}
  <div class="menu" style:left="{menu.x}px" style:top="{menu.y}px" role="menu">
    {#each menu.items as item, i (i)}
      {#if item === 'sep'}
        <div class="menu-sep"></div>
      {:else}
        <button
          role="menuitem"
          class={['menu-item', item.danger && 'danger']}
          disabled={item.disabled}
          onclick={() => {
            menu = null;
            item.action();
          }}>{item.label}</button
        >
      {/if}
    {/each}
  </div>
{/if}

<style>
  .sidebar {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--sidebar-bg);
    min-width: 0;
  }
  .head {
    display: flex;
    align-items: center;
    padding: 8px 8px 8px 12px;
    height: 36px;
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }
  .logo {
    width: 20px;
    height: 20px;
    margin-right: 6px;
  }
  .brand {
    font-weight: 700;
    font-size: 13px;
    margin-right: 10px;
    letter-spacing: 0.01em;
  }
  .title {
    flex: 1;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-muted);
    font-weight: 600;
  }
  .filter {
    padding: 6px 8px;
    border-bottom: 1px solid var(--border);
  }
  .filter input {
    width: 100%;
  }
  .tree {
    flex: 1;
    overflow: auto;
    padding: 4px 0 12px;
    font-size: 12.5px;
  }
  .empty {
    padding: 20px 14px;
    color: var(--text-muted);
    text-align: center;
  }
  .empty p {
    margin: 0 0 10px;
  }
  .empty .btn {
    display: block;
    width: 100%;
    margin-top: 6px;
  }
  .node {
    --depth: 0;
    display: flex;
    align-items: center;
    gap: 5px;
    height: 24px;
    padding-left: calc(6px + var(--depth) * 14px);
    padding-right: 6px;
    cursor: default;
    white-space: nowrap;
    user-select: none;
    outline: none;
  }
  .node:hover {
    background: var(--hover);
  }
  .node:focus-visible {
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .node.conn {
    height: 28px;
    font-weight: 500;
    border-left: 2px solid var(--conn-color);
    padding-left: 4px;
  }
  .node.conn.current {
    background: var(--active-line);
  }
  .node.err {
    color: var(--danger);
    white-space: normal;
    height: auto;
    padding-top: 3px;
    padding-bottom: 3px;
    font-size: 11.5px;
  }
  .node.more {
    width: 100%;
    border: none;
    background: none;
    color: var(--accent);
    font: inherit;
    font-size: 12px;
    cursor: pointer;
    text-align: left;
  }
  .node.more:disabled {
    color: var(--text-muted);
    cursor: default;
  }
  .node.more .count {
    margin-left: auto;
  }
  .node.hint {
    font-size: 11.5px;
  }
  .node.muted {
    color: var(--text-faint);
    font-style: italic;
  }
  .caret {
    width: 14px;
    height: 14px;
    color: var(--text-faint);
    transition: transform 0.1s;
    flex-shrink: 0;
    display: inline-grid;
    place-items: center;
  }
  .caret svg {
    width: 12px;
    height: 12px;
  }
  .caret.open {
    transform: rotate(90deg);
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
    min-width: 0;
  }
  .badge {
    font-size: 9px;
    font-weight: 700;
    padding: 1px 3px;
    border-radius: 3px;
    color: #fff;
    flex-shrink: 0;
    width: 20px;
    text-align: center;
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
  .ico {
    width: 13px;
    height: 13px;
    flex-shrink: 0;
    color: var(--text-muted);
  }
  .ico.table {
    color: #6e9fd8;
  }
  .ico.view {
    color: #b58ad6;
  }
  .colicon {
    width: 13px;
    font-size: 9px;
    color: var(--text-faint);
    text-align: center;
    flex-shrink: 0;
  }
  .type {
    color: var(--text-faint);
    font-size: 11px;
    font-family: var(--mono);
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 45%;
  }
  .count {
    color: var(--text-faint);
    font-size: 11px;
  }
  .live {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--success);
    flex-shrink: 0;
  }
  .actions {
    display: none;
    gap: 2px;
  }
  .node.conn:hover .actions {
    display: flex;
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
  .confirm {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    background: var(--danger-bg);
    font-size: 12px;
    flex-wrap: wrap;
  }
  .menu {
    position: fixed;
    z-index: 100;
    min-width: 190px;
    background: var(--panel-2);
    border: 1px solid var(--border-strong);
    border-radius: 7px;
    padding: 4px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
  }
  .menu-item {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    color: var(--text);
    padding: 5px 10px;
    border-radius: 4px;
    font-size: 12.5px;
    cursor: pointer;
  }
  .menu-item:hover:not(:disabled) {
    background: var(--accent-bg);
  }
  .menu-item:disabled {
    color: var(--text-faint);
    cursor: default;
  }
  .menu-item.danger {
    color: var(--danger);
  }
  .menu-sep {
    height: 1px;
    background: var(--border);
    margin: 4px 2px;
  }
</style>
