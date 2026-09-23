import { ConnectionService, FileService, QueryService, WorkspaceService } from '../../bindings/dbird';
import type { Connection, Tab } from '../../bindings/dbird/internal/store/models';
import type { Result } from '../../bindings/dbird/internal/dbx/models';
import { splitStatements, statementAt, unfilteredDelete, type Dialect, type Statement } from './sqlsplit';

export type { Connection, Tab, Result };

export interface TabRuntime {
  running: boolean;
  activeResult: number;
  error: string;
  lastRunAt: number;
}

export interface ConfirmRequest {
  title: string;
  message: string;
  details: string[];
  confirmLabel: string;
  resolve: (ok: boolean) => void;
}

export interface Toast {
  id: number;
  kind: 'info' | 'error';
  text: string;
}

export function errorText(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === 'string') return e;
  if (e && typeof e === 'object' && 'message' in e) return String((e as { message: unknown }).message);
  return String(e);
}

const idleRuntime: TabRuntime = { running: false, activeResult: 0, error: '', lastRunAt: 0 };

function newID(): string {
  return Math.random().toString(16).slice(2, 10) + Date.now().toString(16).slice(-6);
}

export function emptyConnection(): Connection {
  return {
    id: '',
    name: '',
    driver: 'postgres',
    host: 'localhost',
    port: 5432,
    user: '',
    password: '',
    database: '',
    sslMode: 'prefer',
    url: '',
    color: '',
  };
}

export const defaultPorts: Record<string, number> = { postgres: 5432, mysql: 3306, sqlite: 0 };

class AppState {
  connections = $state<Connection[]>([]);
  connected = $state<Record<string, boolean>>({});
  connecting = $state<Record<string, boolean>>({});
  tabs = $state<Tab[]>([]);
  activeTabId = $state('');
  runtime = $state<Record<string, TabRuntime>>({});
  // Result sets per tab. Raw (not deeply reactive): they can be large and are
  // only ever replaced wholesale.
  results = $state.raw<Record<string, Result[]>>({});
  maxRows = $state(1000);
  fontSize = $state(13);
  toasts = $state<Toast[]>([]);
  // Table -> columns per connection, for autocompletion.
  completions = $state.raw<Record<string, Record<string, string[]>>>({});

  // Pending confirmation dialog, if any.
  confirmation = $state<ConfirmRequest | null>(null);

  // Connection dialog: null = closed.
  editing = $state<Connection | null>(null);

  activeTab = $derived(this.tabs.find((t) => t.id === this.activeTabId));

  #saveTimer: ReturnType<typeof setTimeout> | undefined;
  #toastSeq = 0;

  async init() {
    try {
      const saved = localStorage.getItem('dbird.maxRows');
      if (saved) this.maxRows = Number(saved) || 1000;
      const fs = localStorage.getItem('dbird.fontSize');
      if (fs) this.fontSize = Number(fs) || 13;
    } catch {
      /* storage unavailable */
    }
    const [conns, ws, connected] = await Promise.all([
      ConnectionService.List(),
      WorkspaceService.Load(),
      ConnectionService.Connected(),
    ]);
    this.connections = conns ?? [];
    this.connected = Object.fromEntries((connected ?? []).map((id) => [id, true]));
    const known = new Set(this.connections.map((c) => c.id));
    this.tabs = (ws.tabs ?? []).map((t) => ({ ...t, connectionId: known.has(t.connectionId) ? t.connectionId : '' }));
    for (const t of this.tabs) this.#ensureRt(t.id);
    if (this.tabs.length === 0) this.newTab(this.connections[0]?.id ?? '');
    this.activeTabId = this.tabs.some((t) => t.id === ws.activeTabId) ? ws.activeTabId : this.tabs[0].id;
  }

  toast(text: string, kind: Toast['kind'] = 'info') {
    const id = ++this.#toastSeq;
    this.toasts.push({ id, kind, text });
    setTimeout(() => (this.toasts = this.toasts.filter((t) => t.id !== id)), kind === 'error' ? 6000 : 3000);
  }

  connection(id: string): Connection | undefined {
    return this.connections.find((c) => c.id === id);
  }

  dialect(connId: string): Dialect {
    return (this.connection(connId)?.driver as Dialect) ?? 'postgres';
  }

  // ---- workspace persistence ----

  scheduleSave() {
    clearTimeout(this.#saveTimer);
    this.#saveTimer = setTimeout(() => this.saveNow(), 400);
  }

  saveNow() {
    clearTimeout(this.#saveTimer);
    const tabs = $state.snapshot(this.tabs);
    WorkspaceService.Save({ tabs, activeTabId: this.activeTabId }).catch((e) =>
      console.error('save workspace:', errorText(e)),
    );
  }

  // ---- connections ----

  async reloadConnections() {
    this.connections = (await ConnectionService.List()) ?? [];
  }

  async saveConnection(c: Connection): Promise<Connection> {
    const saved = await ConnectionService.Save(c);
    const wasConnected = this.connected[saved.id];
    await this.reloadConnections();
    if (wasConnected) {
      // Saving closes the pool so new settings apply.
      const connected = await ConnectionService.Connected();
      this.connected = Object.fromEntries((connected ?? []).map((id) => [id, true]));
    }
    return saved;
  }

  async deleteConnection(id: string) {
    await ConnectionService.Delete(id);
    delete this.connected[id];
    for (const t of this.tabs) if (t.connectionId === id) t.connectionId = '';
    await this.reloadConnections();
    this.scheduleSave();
  }

  async connect(id: string): Promise<boolean> {
    if (this.connected[id]) return true;
    this.connecting[id] = true;
    try {
      await ConnectionService.Connect(id);
      this.connected[id] = true;
      this.loadCompletions(id);
      return true;
    } catch (e) {
      this.toast(`Connect failed: ${errorText(e)}`, 'error');
      return false;
    } finally {
      delete this.connecting[id];
    }
  }

  async disconnect(id: string) {
    await ConnectionService.Disconnect(id);
    delete this.connected[id];
  }

  #completionTried = new Set<string>();

  // Loads completions the first time the user types in a tab using connection id.
  ensureCompletions(id: string) {
    if (!id || this.completions[id] || this.#completionTried.has(id)) return;
    this.#completionTried.add(id);
    if (this.connected[id]) this.loadCompletions(id);
    else this.connect(id);
  }

  async loadCompletions(id: string) {
    try {
      const schema = await ConnectionService.DefaultSchema(id);
      if (!schema) return;
      const map = await ConnectionService.Completions(id, schema);
      this.completions = { ...this.completions, [id]: (map ?? {}) as Record<string, string[]> };
    } catch (e) {
      console.warn('completions', e);
    }
  }

  // ---- tabs ----

  newTab(connectionId = this.activeTab?.connectionId ?? '', sql = '', title = ''): Tab {
    const conn = this.connection(connectionId);
    const base = conn ? conn.name : 'Script';
    const taken = new Set(this.tabs.map((t) => t.title));
    let n = 1;
    while (taken.has(`${base} ${n}`)) n++;
    const tab: Tab = {
      id: newID(),
      title: title || `${base} ${n}`,
      connectionId,
      sql,
      filePath: '',
    };
    this.#ensureRt(tab.id);
    this.tabs.push(tab);
    this.activeTabId = tab.id;
    this.scheduleSave();
    return this.tabs[this.tabs.length - 1];
  }

  closeTab(id: string) {
    const idx = this.tabs.findIndex((t) => t.id === id);
    if (idx < 0) return;
    QueryService.CloseTab(id).catch(() => {});
    this.tabs.splice(idx, 1);
    delete this.runtime[id];
    const { [id]: _, ...rest } = this.results;
    this.results = rest;
    if (this.activeTabId === id) {
      const next = this.tabs[idx] ?? this.tabs[idx - 1];
      this.activeTabId = next?.id ?? '';
    }
    if (this.tabs.length === 0) this.newTab('');
    this.scheduleSave();
  }

  // ---- files ----

  async openScript() {
    try {
      const f = await FileService.OpenScript();
      if (!f.path) return;
      const existing = this.tabs.find((t) => t.filePath === f.path);
      if (existing) {
        this.activate(existing.id);
        return;
      }
      const cur = this.activeTab;
      // Reuse an empty untouched tab instead of piling up new ones.
      const tab = cur && !cur.sql.trim() && !cur.filePath ? cur : this.newTab();
      tab.sql = f.content;
      tab.title = f.name;
      tab.filePath = f.path;
      this.scheduleSave();
    } catch (e) {
      this.toast(`Open failed: ${errorText(e)}`, 'error');
    }
  }

  async saveScript(tab: Tab, saveAs = false) {
    try {
      if (tab.filePath && !saveAs) {
        await FileService.SaveScript(tab.filePath, tab.sql);
        this.toast(`Saved ${tab.title}`);
        return;
      }
      const name = tab.filePath ? tab.filePath.split(/[\\/]/).pop()! : `${tab.title.replace(/[^\w.-]+/g, '_')}.sql`;
      const path = await FileService.SaveAs(name, tab.sql, 'sql');
      if (!path) return;
      tab.filePath = path;
      tab.title = path.split(/[\\/]/).pop() ?? tab.title;
      this.scheduleSave();
      this.toast(`Saved ${tab.title}`);
    } catch (e) {
      this.toast(`Save failed: ${errorText(e)}`, 'error');
    }
  }

  async exportResult(r: Result, baseName: string) {
    const esc = (v: string | null) => (v == null ? '' : /[",\n\r]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v);
    const lines = [(r.columns ?? []).map((c) => esc(c.name)).join(',')];
    for (const row of r.rows ?? []) lines.push((row ?? []).map(esc).join(','));
    try {
      const path = await FileService.SaveAs(`${baseName.replace(/[^\w.-]+/g, '_')}.csv`, lines.join('\n') + '\n', 'csv');
      if (path) this.toast(`Exported ${(r.rows ?? []).length} rows`);
    } catch (e) {
      this.toast(`Export failed: ${errorText(e)}`, 'error');
    }
  }

  activate(id: string) {
    this.activeTabId = id;
    this.scheduleSave();
  }

  cycleTab(delta: number) {
    const idx = this.tabs.findIndex((t) => t.id === this.activeTabId);
    if (idx < 0 || this.tabs.length < 2) return;
    this.activate(this.tabs[(idx + delta + this.tabs.length) % this.tabs.length].id);
  }

  moveTab(from: number, to: number) {
    if (from === to) return;
    const [t] = this.tabs.splice(from, 1);
    this.tabs.splice(to, 0, t);
    this.scheduleSave();
  }

  #ensureRt(tabId: string) {
    if (!this.runtime[tabId]) {
      this.runtime[tabId] = { running: false, activeResult: 0, error: '', lastRunAt: 0 };
    }
  }

  // Runtime state of a tab. Created with the tab, so safe to read in $derived.
  rt(tabId: string): TabRuntime {
    return this.runtime[tabId] ?? idleRuntime;
  }

  // ---- execution ----

  // Runs the selection, or the statement at the cursor, or (script=true)
  // everything. Returns the document ranges that were executed.
  execute(tab: Tab, sel: { from: number; to: number; head: number }, script: boolean): { from: number; to: number }[] {
    if (!tab.connectionId) {
      this.toast('Pick a connection for this tab first', 'error');
      return [];
    }
    const dialect = this.dialect(tab.connectionId);
    let stmts: Statement[];
    if (sel.from !== sel.to) {
      const text = tab.sql.slice(sel.from, sel.to);
      stmts = script
        ? splitStatements(text, dialect).map((s) => ({ ...s, from: s.from + sel.from, to: s.to + sel.from }))
        : [{ text: text.trim().replace(/;\s*$/, ''), from: sel.from, to: sel.to }];
    } else if (script) {
      stmts = splitStatements(tab.sql, dialect);
    } else {
      const s = statementAt(splitStatements(tab.sql, dialect), sel.head);
      stmts = s ? [s] : [];
    }
    stmts = stmts.filter((s) => s.text.trim() !== '');
    if (stmts.length === 0 || this.rt(tab.id).running) return [];
    this.run(
      tab,
      stmts.map((s) => s.text),
      script,
    );
    return stmts;
  }

  ask(req: Omit<ConfirmRequest, 'resolve'>): Promise<boolean> {
    this.confirmation?.resolve(false);
    return new Promise((resolve) => {
      this.confirmation = {
        ...req,
        resolve: (ok) => {
          this.confirmation = null;
          resolve(ok);
        },
      };
    });
  }

  // Asks before running DELETE statements that have no WHERE clause.
  async #confirmDangerous(tab: Tab, statements: string[]): Promise<boolean> {
    const dialect = this.dialect(tab.connectionId);
    const risky = statements
      .map((sql) => ({ sql, table: unfilteredDelete(sql, dialect) }))
      .filter((r): r is { sql: string; table: string } => r.table !== null);
    if (risky.length === 0) return true;
    const conn = this.connection(tab.connectionId);
    const tables = [...new Set(risky.map((r) => r.table))];
    return this.ask({
      title: 'Delete all rows?',
      message:
        (risky.length === 1
          ? `This DELETE has no WHERE clause and will remove every row from ${tables[0]}`
          : `${risky.length} DELETE statements have no WHERE clause and will remove every row from ${tables.join(', ')}`) +
        (conn ? ` on “${conn.name}”.` : '.'),
      details: risky.map((r) => r.sql),
      confirmLabel: tables.length === 1 ? `Delete all rows from ${tables[0]}` : 'Delete all rows',
    });
  }

  async run(tab: Tab, statements: string[], continueOnError = false) {
    this.#ensureRt(tab.id);
    const rt = this.runtime[tab.id];
    if (rt.running) return;
    if (!(await this.#confirmDangerous(tab, statements))) return;
    rt.running = true;
    rt.error = '';
    try {
      if (!(await this.connect(tab.connectionId))) {
        rt.error = 'Not connected';
        return;
      }
      const results = await QueryService.Run(tab.id, tab.connectionId, statements, this.maxRows, continueOnError);
      const list = results ?? [];
      this.results = { ...this.results, [tab.id]: list };
      // Focus the first failing result, else the last result set.
      let active = list.findIndex((r) => r.error);
      if (active < 0) {
        active = list.length - 1;
        for (let i = list.length - 1; i >= 0; i--) {
          if (list[i].hasResultSet) {
            active = i;
            break;
          }
        }
      }
      rt.activeResult = Math.max(0, active);
      rt.lastRunAt = Date.now();
      // DDL may have changed the schema; refresh completions quietly.
      if (list.some((r) => !r.hasResultSet && !r.error)) this.loadCompletions(tab.connectionId);
    } catch (e) {
      rt.error = errorText(e);
      this.results = { ...this.results, [tab.id]: [] };
    } finally {
      rt.running = false;
    }
  }

  async cancel(tabId: string) {
    try {
      await QueryService.Cancel(tabId);
    } catch (e) {
      this.toast(errorText(e), 'error');
    }
  }

  zoom(delta: number) {
    this.fontSize = Math.max(9, Math.min(28, this.fontSize + delta));
    try {
      localStorage.setItem('dbird.fontSize', String(this.fontSize));
    } catch {
      /* ignore */
    }
  }

  setMaxRows(n: number) {
    this.maxRows = n;
    try {
      localStorage.setItem('dbird.maxRows', String(n));
    } catch {
      /* ignore */
    }
  }
}

export const app = new AppState();
