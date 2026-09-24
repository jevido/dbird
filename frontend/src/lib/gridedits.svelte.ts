// Cell edits made in the result grid, kept per result set until they are
// saved (UPDATE statements through QueryService.SaveEdits) or discarded.
import { QueryService } from '../../bindings/dbird';
import type { Result } from './state.svelte';
import { app, errorText } from './state.svelte';

export class GridEdits {
  // New values by row index (in result.rows) and column index; null = NULL.
  cells = $state<Record<number, Record<number, string | null>>>({});
  saving = $state(false);
  error = $state('');
  // Bumped when saved values are written into result.rows, which isn't
  // reactive itself.
  version = $state(0);

  count = $derived(Object.values(this.cells).reduce((n, row) => n + Object.keys(row).length, 0));
  rowCount = $derived(Object.keys(this.cells).length);

  private result: Result;

  constructor(result: Result) {
    this.result = result;
  }

  // The value shown for a cell: edited, else as queried.
  value(r: number, c: number): string | null {
    const row = this.cells[r];
    if (row && c in row) return row[c];
    return (this.result.rows?.[r]?.[c] ?? null) as string | null;
  }

  edited(r: number, c: number): boolean {
    return !!this.cells[r] && c in this.cells[r];
  }

  // Sets a cell; setting it back to its queried value drops the edit.
  set(r: number, c: number, v: string | null) {
    const original = (this.result.rows?.[r]?.[c] ?? null) as string | null;
    const row = { ...(this.cells[r] ?? {}) };
    if (v === original) delete row[c];
    else row[c] = v;
    const cells = { ...this.cells };
    if (Object.keys(row).length) cells[r] = row;
    else delete cells[r];
    this.cells = cells;
    this.error = '';
  }

  revert(r: number, c: number) {
    this.set(r, c, (this.result.rows?.[r]?.[c] ?? null) as string | null);
  }

  discard() {
    this.cells = {};
    this.error = '';
  }

  async save(): Promise<boolean> {
    const ed = this.result.editable;
    const connId = resultConnections.get(this.result);
    if (!ed || !connId || this.count === 0 || this.saving) return false;
    const rows = (this.result.rows ?? []) as (string | null)[][];
    const key = ed.key ?? [];
    const columns = ed.columns ?? [];
    const req = {
      schema: ed.schema,
      table: ed.table,
      keyColumns: key.map((k) => columns[k]),
      rows: Object.entries(this.cells).map(([r, cols]) => ({
        // The key as queried, so rows whose key was edited are still found.
        key: key.map((k) => rows[Number(r)][k]),
        changes: Object.fromEntries(Object.entries(cols).map(([c, v]) => [columns[Number(c)], v])),
      })),
    };
    this.saving = true;
    this.error = '';
    try {
      const n = await QueryService.SaveEdits(connId, req);
      for (const [r, cols] of Object.entries(this.cells)) {
        for (const [c, v] of Object.entries(cols)) rows[Number(r)][Number(c)] = v;
      }
      this.cells = {};
      this.version++;
      app.toast(`Saved ${n} row${n === 1 ? '' : 's'} to ${ed.table}`);
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

// Number of unsaved cell edits across results.
export function pendingEdits(results: Result[] | undefined): number {
  return (results ?? []).reduce((n, r) => n + (edits.get(r)?.count ?? 0), 0);
}
