// Changes made in the result grid, kept per result set until they are saved
// (QueryService.SaveEdits) or discarded: edited cells, new rows and rows
// marked for deletion, with undo/redo.
import { QueryService } from '../../bindings/dbird';
import type { EditColumn, EditRequest } from '../../bindings/dbird/internal/dbx/models';
import type { Result } from './state.svelte';
import { app, errorText } from './state.svelte';

type Cells = Record<number, Record<number, string | null>>;
// Values of a new row by column index; columns left out get their default.
type NewRow = Record<number, string | null>;

interface Snapshot {
  cells: Cells;
  added: NewRow[];
  deleted: Record<number, true>;
}

const UNDO_LIMIT = 200;

export class GridEdits {
  // Edited values of existing rows, by row index (in result.rows) and column.
  cells = $state<Cells>({});
  // New rows, shown after the existing ones.
  added = $state<NewRow[]>([]);
  // Existing rows marked for deletion.
  deleted = $state<Record<number, true>>({});
  saving = $state(false);
  error = $state('');
  // Bumped when result.rows changes (saved values, loaded pages), which
  // isn't reactive itself.
  version = $state(0);
  #undo = $state<Snapshot[]>([]);
  #redo = $state<Snapshot[]>([]);

  editedCount = $derived(Object.values(this.cells).reduce((n, row) => n + Object.keys(row).length, 0));
  deletedCount = $derived(Object.keys(this.deleted).length);
  count = $derived(this.editedCount + this.added.length + this.deletedCount);
  canUndo = $derived(this.#undo.length > 0);
  canRedo = $derived(this.#redo.length > 0);

  private result: Result;

  constructor(result: Result) {
    this.result = result;
  }

  get rows(): (string | null)[][] {
    return (this.result.rows ?? []) as (string | null)[][];
  }

  // Number of existing rows; new rows have indices from here on.
  get base(): number {
    void this.version;
    return this.rows.length;
  }

  get total(): number {
    return this.base + this.added.length;
  }

  column(c: number): EditColumn | undefined {
    return this.result.editable?.columns?.[c] ?? undefined;
  }

  canEdit(c: number): boolean {
    return !!this.column(c)?.name;
  }

  isAdded(r: number): boolean {
    return r >= this.base;
  }

  isDeleted(r: number): boolean {
    return !!this.deleted[r];
  }

  // The value shown for a cell: edited, else as queried. A new row's column
  // that wasn't set is undefined (it gets its default).
  value(r: number, c: number): string | null | undefined {
    if (r >= this.base) {
      const row = this.added[r - this.base];
      return row && c in row ? row[c] : undefined;
    }
    const row = this.cells[r];
    if (row && c in row) return row[c];
    return this.rows[r]?.[c] ?? null;
  }

  edited(r: number, c: number): boolean {
    if (r >= this.base) return c in (this.added[r - this.base] ?? {});
    return !!this.cells[r] && c in this.cells[r];
  }

  #snapshot() {
    this.#undo = [...this.#undo.slice(-UNDO_LIMIT + 1), { cells: this.cells, added: this.added, deleted: this.deleted }];
    this.#redo = [];
    this.error = '';
  }

  #restore(s: Snapshot) {
    this.cells = s.cells;
    this.added = s.added;
    this.deleted = s.deleted;
    this.error = '';
  }

  undo() {
    const s = this.#undo.at(-1);
    if (!s) return;
    this.#redo = [...this.#redo, { cells: this.cells, added: this.added, deleted: this.deleted }];
    this.#undo = this.#undo.slice(0, -1);
    this.#restore(s);
  }

  redo() {
    const s = this.#redo.at(-1);
    if (!s) return;
    this.#undo = [...this.#undo, { cells: this.cells, added: this.added, deleted: this.deleted }];
    this.#redo = this.#redo.slice(0, -1);
    this.#restore(s);
  }

  // Sets cells as one undo step. Setting a cell back to its queried value
  // drops the edit.
  setMany(changes: { r: number; c: number; v: string | null }[]) {
    changes = changes.filter(({ c }) => this.canEdit(c));
    if (changes.length === 0) return;
    this.#snapshot();
    const cells = { ...this.cells };
    const added = [...this.added];
    for (const { r, c, v } of changes) {
      if (r >= this.base) {
        const i = r - this.base;
        if (added[i]) added[i] = { ...added[i], [c]: v };
        continue;
      }
      const original = this.rows[r]?.[c] ?? null;
      const row = { ...(cells[r] ?? {}) };
      if (v === original) delete row[c];
      else row[c] = v;
      if (Object.keys(row).length) cells[r] = row;
      else delete cells[r];
    }
    this.cells = cells;
    this.added = added;
  }

  set(r: number, c: number, v: string | null) {
    this.setMany([{ r, c, v }]);
  }

  // Undoes the edit of one cell (for a new row: back to its default).
  revert(r: number, c: number) {
    if (r >= this.base) {
      const i = r - this.base;
      if (!this.added[i] || !(c in this.added[i])) return;
      this.#snapshot();
      const row = { ...this.added[i] };
      delete row[c];
      this.added = this.added.map((x, j) => (j === i ? row : x));
      return;
    }
    this.set(r, c, this.rows[r]?.[c] ?? null);
  }

  // Adds new rows (with the given values) and returns the index of the first.
  addRows(rows: NewRow[] = [{}]): number {
    this.#snapshot();
    const first = this.total;
    this.added = [...this.added, ...rows];
    return first;
  }

  // Marks existing rows for deletion (or unmarks them when all already are)
  // and removes new rows.
  toggleDelete(rows: number[]) {
    if (rows.length === 0) return;
    this.#snapshot();
    const existing = rows.filter((r) => r < this.base);
    const deleted = { ...this.deleted };
    const all = existing.length > 0 && existing.every((r) => deleted[r]);
    for (const r of existing) {
      if (all) delete deleted[r];
      else deleted[r] = true;
    }
    const drop = new Set(rows.filter((r) => r >= this.base).map((r) => r - this.base));
    this.added = this.added.filter((_, i) => !drop.has(i));
    this.deleted = deleted;
  }

  discard() {
    if (this.count === 0) return;
    this.#snapshot();
    this.cells = {};
    this.added = [];
    this.deleted = {};
  }

  // Checks what the database would reject anyway, with clearer messages.
  validate(): string {
    const cols = this.result.editable?.columns ?? [];
    for (const [i, row] of this.added.entries()) {
      for (const [c, col] of cols.entries()) {
        if (!col.name || col.nullable || col.hasDefault) continue;
        if (row[c] == null) return `New row ${i + 1}: ${col.name} is required`;
      }
    }
    for (const [r, row] of Object.entries(this.cells)) {
      for (const [c, v] of Object.entries(row)) {
        const col = cols[Number(c)];
        if (v === null && col && !col.nullable) return `Row ${Number(r) + 1}: ${col.name} can't be NULL`;
      }
    }
    return '';
  }

  // Updated rows (indices in result.rows), in request order.
  #updatedRows(): number[] {
    return Object.keys(this.cells)
      .map(Number)
      .filter((r) => !this.deleted[r]);
  }

  request(): EditRequest | null {
    const ed = this.result.editable;
    if (!ed) return null;
    const cols = ed.columns ?? [];
    const key = ed.key ?? [];
    const rows = this.rows;
    const keyOf = (r: number) => key.map((k) => rows[r][k]);
    const named = (vals: Record<number, string | null>) =>
      Object.fromEntries(Object.entries(vals).map(([c, v]) => [cols[Number(c)].name, v]));
    return {
      schema: ed.schema,
      table: ed.table,
      keyColumns: key.map((k) => cols[k].name),
      columns: cols.map((c) => c.source),
      updates: this.#updatedRows().map((r) => ({
        // The key as queried, so rows whose key was edited are still found.
        key: keyOf(r),
        changes: named(this.cells[r]),
        original: Object.fromEntries(Object.keys(this.cells[r]).map((c) => [cols[Number(c)].name, rows[r][Number(c)]])),
      })),
      inserts: this.added.map((row) => ({ values: named(row) })),
      deletes: Object.keys(this.deleted).map((r) => ({ key: keyOf(Number(r)) })),
    };
  }

  async preview(): Promise<string[]> {
    const req = this.request();
    const connId = resultConnections.get(this.result);
    if (!req || !connId) return [];
    return (await QueryService.PreviewEdits(connId, req)) ?? [];
  }

  async save(): Promise<boolean> {
    const req = this.request();
    const connId = resultConnections.get(this.result);
    if (!req || !connId || this.count === 0 || this.saving) return false;
    const invalid = this.validate();
    if (invalid) {
      this.error = invalid;
      return false;
    }
    this.saving = true;
    this.error = '';
    const updated = this.#updatedRows();
    try {
      const res = await QueryService.SaveEdits(connId, req);
      if (!res) return false;
      const rows = this.rows;
      const width = this.result.columns?.length ?? 0;
      const cols = this.result.editable?.columns ?? [];
      // Values as now stored; columns that aren't table columns keep theirs.
      const merge = (old: (string | null)[] | undefined, fresh: (string | null)[] | null | undefined) =>
        Array.from({ length: width }, (_, c) => (fresh && cols[c]?.source ? fresh[c] : (old?.[c] ?? null)));
      updated.forEach((r, i) => (rows[r] = merge(rows[r], res.updated?.[i])));
      for (const r of Object.keys(this.deleted).map(Number).sort((a, b) => b - a)) rows.splice(r, 1);
      for (const [i, fresh] of (res.inserted ?? []).entries()) {
        const typed = this.added[i] ?? {};
        rows.push(merge(Array.from({ length: width }, (_, c) => typed[c] ?? null), fresh));
      }
      const parts = [
        updated.length && `${updated.length} updated`,
        res.inserted?.length && `${res.inserted.length} added`,
        res.deleted && `${res.deleted} deleted`,
      ].filter(Boolean);
      this.cells = {};
      this.added = [];
      this.deleted = {};
      this.#undo = [];
      this.#redo = [];
      this.version++;
      app.toast(`Saved to ${req.table}: ${parts.join(', ')}`);
      return true;
    } catch (e) {
      this.error = errorText(e);
      return false;
    } finally {
      this.saving = false;
    }
  }
}

const edits = new WeakMap<Result, GridEdits>();
// The connection a result was queried on, so edits are saved there even if
// the tab has switched connections since.
const resultConnections = new WeakMap<Result, string>();

export function editsFor(result: Result): GridEdits {
  let e = edits.get(result);
  if (!e) {
    e = new GridEdits(result);
    edits.set(result, e);
  }
  return e;
}

export function setResultConnection(result: Result, connId: string) {
  resultConnections.set(result, connId);
}

export function resultConnection(result: Result): string {
  return resultConnections.get(result) ?? '';
}

// Number of unsaved changes across results.
export function pendingEdits(results: Result[] | undefined): number {
  return (results ?? []).reduce((n, r) => n + (edits.get(r)?.count ?? 0), 0);
}

// How a browsable result was filtered and sorted, carried over to the
// results that replace it.
export interface BrowseState {
  // The query as the user wrote it; filters and sorting rewrite this.
  base: string;
  filter: string;
  orderBy: number; // 1-based result column, 0 = the query's own order
  desc: boolean;
  // Set when the last page came back short, so there is nothing more to load.
  exhausted: boolean;
}

const browseStates = new WeakMap<Result, BrowseState>();

export function browseState(result: Result): BrowseState {
  let s = browseStates.get(result);
  if (!s) {
    s = { base: result.sql, filter: '', orderBy: 0, desc: false, exhausted: false };
    browseStates.set(result, s);
  }
  return s;
}

export function setBrowseState(result: Result, s: BrowseState) {
  browseStates.set(result, s);
}
