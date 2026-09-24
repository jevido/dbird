// Monaco setup: only the editor core, the editor features a SQL editor uses and
// the three SQL languages are bundled. The features are picked one by one
// instead of features/register.all.js, which also brings the diff editor,
// color picker, code lens, inlay hints, rename, GPU rendering and other parts
// that need language services dbird doesn't have.
import * as monaco from 'monaco-editor/editor/editor.api.js';
import 'monaco-editor/features/codicon/register.js';
import 'monaco-editor/features/bracketMatching/register.js';
import 'monaco-editor/features/caretOperations/register.js';
import 'monaco-editor/features/clipboard/register.js';
import 'monaco-editor/features/comment/register.js';
import 'monaco-editor/features/contextmenu/register.js';
import 'monaco-editor/features/cursorUndo/register.js';
import 'monaco-editor/features/dnd/register.js';
import 'monaco-editor/features/find/register.js';
import 'monaco-editor/features/folding/register.js';
import 'monaco-editor/features/fontZoom/register.js';
import 'monaco-editor/features/gotoLine/register.js';
import 'monaco-editor/features/hover/register.js';
import 'monaco-editor/features/indentation/register.js';
import 'monaco-editor/features/lineSelection/register.js';
import 'monaco-editor/features/linesOperations/register.js';
import 'monaco-editor/features/multicursor/register.js';
import 'monaco-editor/features/placeholderText/register.js';
import 'monaco-editor/features/quickCommand/register.js';
import 'monaco-editor/features/readOnlyMessage/register.js';
import 'monaco-editor/features/smartSelect/register.js';
import 'monaco-editor/features/snippet/register.js';
// The autocomplete popup. features/suggest only adds suggestions as inline
// ghost text; with register.all the popup came in through inlineCompletions.
import 'monaco-editor/editor/contrib/suggest/browser/suggestController.js';
import 'monaco-editor/features/toggleTabFocusMode/register.js';
import 'monaco-editor/features/tokenization/register.js';
import 'monaco-editor/features/wordHighlighter/register.js';
import 'monaco-editor/features/wordOperations/register.js';
import 'monaco-editor/features/wordPartOperations/register.js';
import 'monaco-editor/languages/definitions/sql/register.js';
import 'monaco-editor/languages/definitions/mysql/register.js';
import { conf as pgsqlConf, language as pgsqlBase } from 'monaco-editor/languages/definitions/pgsql/pgsql.js';
import { language as mysqlLang } from 'monaco-editor/languages/definitions/mysql/mysql.js';
import { language as sqlLang } from 'monaco-editor/languages/definitions/sql/sql.js';
import EditorWorker from 'monaco-editor/editor/editor.worker.js?worker';
import { ConnectionService } from '../../bindings/dbird';
import type { CompletionSetup } from '../../bindings/dbird/models';
import { quoteIdent } from './sqlutil';

export { monaco };

self.MonacoEnvironment = {
  getWorker: () => new EditorWorker(),
};

// Monaco's PostgreSQL grammar only knows reserved words, so everyday keywords
// such as UPDATE, DELETE, ALTER and COMMIT render as plain identifiers. dbird
// registers an extended copy under its own id.
const extraPgKeywords = (
  'INSERT UPDATE DELETE MERGE VALUES SET BY RECURSIVE ALTER DROP VIEW INDEX SCHEMA FUNCTION PROCEDURE TRIGGER ' +
  'SEQUENCE TYPE EXTENSION BEGIN COMMIT ROLLBACK SAVEPOINT RELEASE TRANSACTION TRUNCATE EXPLAIN VACUUM REVOKE ' +
  'CONFLICT NOTHING KEY CASCADE RESTRICT OVER PARTITION RETURNS LANGUAGE REPLACE IF EXISTS TEMPORARY TEMP ' +
  'MATERIALIZED REFRESH ADD COLUMN RENAME OWNER COMMENT COPY LOCK CLUSTER REINDEX SHOW RESET DECLARE CURSOR ' +
  'FETCH MOVE CLOSE LISTEN NOTIFY PREPARE EXECUTE DEALLOCATE CALL NULLS FIRST LAST FILTER WITHIN ORDINALITY'
).split(' ');
const pgsqlLang = {
  ...pgsqlBase,
  keywords: [...new Set([...(pgsqlBase.keywords as string[]), ...extraPgKeywords])].filter(
    (k) => !(pgsqlBase.operators as string[]).includes(k),
  ),
};
// Registrations are undone when this module is hot-reloaded in dev, so they
// don't pile up.
const disposables: monaco.IDisposable[] = [];
import.meta.hot?.dispose(() => disposables.forEach((d) => d.dispose()));

if (!monaco.languages.getLanguages().some((l) => l.id === 'dbird-pgsql')) {
  monaco.languages.register({ id: 'dbird-pgsql', aliases: ['PostgreSQL'] });
}
disposables.push(
  monaco.languages.setLanguageConfiguration('dbird-pgsql', pgsqlConf as monaco.languages.LanguageConfiguration),
  monaco.languages.setMonarchTokensProvider('dbird-pgsql', pgsqlLang as unknown as monaco.languages.IMonarchLanguage),
);

// Monaco language id per dbird driver.
export const languageFor: Record<string, string> = { postgres: 'dbird-pgsql', mysql: 'mysql', sqlite: 'sql' };

monaco.editor.defineTheme('dbird-dark', {
  base: 'vs-dark',
  inherit: true,
  rules: [
    { token: 'keyword', foreground: 'cf8ef0', fontStyle: 'bold' },
    { token: 'keyword.sql', foreground: 'cf8ef0', fontStyle: 'bold' },
    { token: 'operator', foreground: 'aab0c0' },
    { token: 'operator.sql', foreground: 'aab0c0' },
    { token: 'string', foreground: '9fd28a' },
    { token: 'string.sql', foreground: '9fd28a' },
    { token: 'number', foreground: 'f0a869' },
    { token: 'number.sql', foreground: 'f0a869' },
    { token: 'comment', foreground: '6c7285', fontStyle: 'italic' },
    { token: 'comment.sql', foreground: '6c7285', fontStyle: 'italic' },
    { token: 'predefined', foreground: '6ec3d6' },
    { token: 'predefined.sql', foreground: '6ec3d6' },
    { token: 'identifier.quote', foreground: 'e6c07b' },
    { token: 'identifier.quote.sql', foreground: 'e6c07b' },
    { token: 'delimiter', foreground: 'aab0c0' },
    { token: 'delimiter.sql', foreground: 'aab0c0' },
  ],
  colors: {
    'editor.background': '#1b1c20',
    'editor.foreground': '#dcdee4',
    'editorLineNumber.foreground': '#666a78',
    'editorLineNumber.activeForeground': '#9a9dab',
    'editorCursor.foreground': '#5b8def',
    'editor.selectionBackground': '#5484e659',
    'editor.inactiveSelectionBackground': '#5484e633',
    'editor.lineHighlightBackground': '#ffffff09',
    'editor.lineHighlightBorder': '#00000000',
    'editorWidget.background': '#2c2d33',
    'editorWidget.border': '#3c3d45',
    'editorSuggestWidget.background': '#2c2d33',
    'editorSuggestWidget.border': '#3c3d45',
    'editorSuggestWidget.selectedBackground': '#5b8def33',
    'editorGutter.background': '#1b1c20',
    'scrollbarSlider.background': '#3a3b4288',
    'scrollbarSlider.hoverBackground': '#4a4b54aa',
    'focusBorder': '#5b8def',
  },
});

// ---- completion ----
//
// Each editor model has a completion source: the tab's connection and the
// setup the backend chose for it (see ConnectionService.Completion):
//   preload  every table and column of the default schema is in memory
//   lookup   table names are fetched by prefix as you type, and columns only
//            for tables the statement references; both are cached
//   off      keywords and functions only

export interface CompletionSource {
  connId: string;
  driver: string;
  setup: CompletionSetup | undefined;
}

const sources = new Map<string, CompletionSource>();

export function setModelSource(model: monaco.editor.ITextModel, src: CompletionSource) {
  sources.set(model.uri.toString(), src);
}

export function forgetModel(model: monaco.editor.ITextModel) {
  sources.delete(model.uri.toString());
}

// Lookup-mode caches, keyed by connection.
const columnCache = new Map<string, Promise<string[]>>(); // conn \0 schema \0 table
const prefixCache = new Map<string, { at: number; names: Promise<string[]> }>(); // conn \0 schema \0 prefix
const PREFIX_TTL = 30_000;
const PREFIX_LIMIT = 100; // must match ConnectionService.CompleteTables

// Drops cached lookups for a connection, e.g. after DDL or a refresh.
export function invalidateCompletionCache(connId: string) {
  for (const m of [columnCache, prefixCache]) {
    for (const k of m.keys()) if (k.startsWith(connId + '\0')) m.delete(k);
  }
}

function lookupColumns(connId: string, schema: string, table: string): Promise<string[]> {
  const key = `${connId}\0${schema}\0${table}`;
  let p = columnCache.get(key);
  if (!p) {
    p = ConnectionService.TableColumns(connId, schema, table)
      .then((c) => c ?? [])
      .catch(() => {
        columnCache.delete(key); // unknown table (or a typo): retry later
        return [];
      });
    columnCache.set(key, p);
  }
  return p;
}

async function lookupTables(connId: string, schema: string, prefix: string): Promise<string[]> {
  const now = Date.now();
  // A cached shorter prefix that returned fewer than the limit already holds
  // every match for this longer prefix.
  for (let n = prefix.length; n >= 0; n--) {
    const hit = prefixCache.get(`${connId}\0${schema}\0${prefix.slice(0, n)}`);
    if (hit && now - hit.at < PREFIX_TTL) {
      const names = await hit.names;
      if (n === prefix.length) return names;
      if (names.length < PREFIX_LIMIT) {
        const p = prefix.toLowerCase();
        return names.filter((t) => t.toLowerCase().startsWith(p));
      }
    }
  }
  const names = ConnectionService.CompleteTables(connId, schema, prefix)
    .then((r) => r ?? [])
    .catch(() => []);
  prefixCache.set(`${connId}\0${schema}\0${prefix}`, { at: now, names });
  return names;
}

const words: Record<string, { keywords: string[]; functions: string[] }> = {
  'dbird-pgsql': { keywords: pgsqlLang.keywords, functions: pgsqlBase.builtinFunctions as string[] },
  mysql: { keywords: mysqlLang.keywords as string[], functions: mysqlLang.builtinFunctions as string[] },
  sql: { keywords: sqlLang.keywords as string[], functions: sqlLang.builtinFunctions as string[] },
};

interface TableRef {
  schema?: string;
  table: string;
}

// Finds "from/join/update/into [schema.]table [as] [alias]" references and
// maps both table names and aliases (lower-cased) to them.
function tableRefs(text: string, driver: string): Map<string, TableRef> {
  const ident = String.raw`(?:"[^"]+"|\x60[^\x60]+\x60|\[[^\]]+\]|[\w$]+)`;
  const re = new RegExp(String.raw`\b(?:from|join|update|into)\s+(${ident})(?:\s*\.\s*(${ident}))?(?:\s+(?:as\s+)?(\w+))?`, 'gi');
  const reserved = /^(where|on|join|left|right|inner|outer|cross|full|group|order|limit|set|values|using|natural|union|select|returning|lateral)$/i;
  const unquote = (s: string) => {
    if (/^["`[]/.test(s)) return s.slice(1, -1);
    return driver === 'postgres' ? s.toLowerCase() : s; // Postgres folds unquoted names
  };
  const out = new Map<string, TableRef>();
  for (const m of text.matchAll(re)) {
    const ref: TableRef = m[2] ? { schema: unquote(m[1]), table: unquote(m[2]) } : { table: unquote(m[1]) };
    if (reserved.test(ref.table)) continue;
    out.set(ref.table.toLowerCase(), ref);
    if (m[3] && !reserved.test(m[3])) out.set(m[3].toLowerCase(), ref);
  }
  return out;
}

for (const lang of Object.keys(words)) {
  disposables.push(monaco.languages.registerCompletionItemProvider(lang, {
    triggerCharacters: ['.'],
    async provideCompletionItems(model, position) {
      const src = sources.get(model.uri.toString());
      const setup = src?.setup;
      const mode = setup?.mode ?? 'off';
      const word = model.getWordUntilPosition(position);
      const range = new monaco.Range(position.lineNumber, word.startColumn, position.lineNumber, word.endColumn);
      const K = monaco.languages.CompletionItemKind;
      const before = model.getLineContent(position.lineNumber).slice(0, word.startColumn - 1);
      const quote = (name: string) => (src ? quoteIdent(src.driver, name) : name);
      const table = (name: string, detail: string, sort = '1') =>
        ({ label: name, kind: K.Struct, insertText: quote(name), detail, range, sortText: sort + name }) as monaco.languages.CompletionItem;
      const column = (name: string, detail: string, sort = '2') =>
        ({ label: name, kind: K.Field, insertText: quote(name), detail, range, sortText: sort + name }) as monaco.languages.CompletionItem;
      const keywords = () => [
        ...words[lang].keywords.map((kw) => ({ label: kw, kind: K.Keyword, insertText: kw, range, sortText: '3' + kw })),
        ...words[lang].functions.map((fn) => ({ label: fn, kind: K.Function, insertText: fn, range, sortText: '4' + fn })),
      ];

      if (!src || !setup || mode === 'off') {
        return /\.$/.test(before) ? { suggestions: [] } : { suggestions: keywords() };
      }
      const refs = tableRefs(model.getValue(), src.driver);
      const dot = /(["`]?)([\w$]+)\1\.$/.exec(before);

      if (mode === 'preload') {
        const cols = setup.columns ?? {};
        const find = (name: string) => Object.keys(cols).find((t) => t.toLowerCase() === name.toLowerCase());
        if (dot) {
          const ref = refs.get(dot[2].toLowerCase());
          const t = ref && !ref.schema ? find(ref.table) : undefined;
          return { suggestions: t ? (cols[t] ?? []).map((c) => column(c, t)) : [] };
        }
        const suggestions: monaco.languages.CompletionItem[] = [];
        for (const [t, cs] of Object.entries(cols)) {
          suggestions.push(table(t, 'table'));
          for (const c of cs ?? []) suggestions.push(column(c, t));
        }
        return { suggestions: [...suggestions, ...keywords()] };
      }

      // lookup mode
      if (dot) {
        const ref = refs.get(dot[2].toLowerCase());
        if (ref) {
          const cols = await lookupColumns(src.connId, ref.schema ?? setup.schema, ref.table);
          return { suggestions: cols.map((c) => column(c, ref.table)) };
        }
        // Not a known table or alias: treat it as a schema name.
        const schema = src.driver === 'postgres' ? dot[2].toLowerCase() : dot[2];
        const names = await lookupTables(src.connId, schema, word.word);
        return { suggestions: names.map((t) => table(t, schema)), incomplete: names.length >= PREFIX_LIMIT };
      }
      const [names, ...colLists] = await Promise.all([
        lookupTables(src.connId, setup.schema, word.word),
        ...[...new Set(refs.values())].map((r) =>
          lookupColumns(src.connId, r.schema ?? setup.schema, r.table).then((cs) => cs.map((c) => column(c, r.table))),
        ),
      ]);
      return {
        suggestions: [...(names as string[]).map((t) => table(t, 'table')), ...colLists.flat(), ...keywords()],
        // Ask again as the prefix grows so the database can narrow the list.
        incomplete: true,
      };
    },
  }));
}
