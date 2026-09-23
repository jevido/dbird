// Monaco setup: only the editor core, its standard features and the three SQL
// languages are bundled (no TypeScript/CSS/HTML/JSON language services).
import * as monaco from 'monaco-editor/editor/editor.api.js';
import 'monaco-editor/features/register.all.js';
import 'monaco-editor/editor/standalone/browser/quickAccess/standaloneCommandsQuickAccess.js';
import 'monaco-editor/editor/standalone/browser/quickAccess/standaloneGotoLineQuickAccess.js';
import 'monaco-editor/languages/definitions/sql/register.js';
import 'monaco-editor/languages/definitions/mysql/register.js';
import { conf as pgsqlConf, language as pgsqlBase } from 'monaco-editor/languages/definitions/pgsql/pgsql.js';
import { language as mysqlLang } from 'monaco-editor/languages/definitions/mysql/mysql.js';
import { language as sqlLang } from 'monaco-editor/languages/definitions/sql/sql.js';
import EditorWorker from 'monaco-editor/editor/editor.worker.js?worker';

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
    { token: 'operator', foreground: 'aab0c0' },
    { token: 'string', foreground: '9fd28a' },
    { token: 'number', foreground: 'f0a869' },
    { token: 'comment', foreground: '6c7285', fontStyle: 'italic' },
    { token: 'predefined', foreground: '6ec3d6' },
    { token: 'identifier.quote', foreground: 'e6c07b' },
    { token: 'delimiter', foreground: 'aab0c0' },
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

type Schema = Record<string, string[]>;

// Table -> columns per model, set by the editor component.
const schemas = new Map<string, Schema>();

export function setModelSchema(model: monaco.editor.ITextModel, schema: Schema | undefined) {
  schemas.set(model.uri.toString(), schema ?? {});
}

export function forgetModel(model: monaco.editor.ITextModel) {
  schemas.delete(model.uri.toString());
}

const words: Record<string, { keywords: string[]; functions: string[] }> = {
  'dbird-pgsql': { keywords: pgsqlLang.keywords, functions: pgsqlBase.builtinFunctions as string[] },
  mysql: { keywords: mysqlLang.keywords as string[], functions: mysqlLang.builtinFunctions as string[] },
  sql: { keywords: sqlLang.keywords as string[], functions: sqlLang.builtinFunctions as string[] },
};

// Maps aliases used in the statement ("from users u", "join orders as o") to table names.
function aliases(text: string, schema: Schema): Map<string, string> {
  const out = new Map<string, string>();
  const re = /\b(?:from|join|update|into)\s+(?:[\w"`]+\.)?["`]?(\w+)["`]?(?:\s+(?:as\s+)?(\w+))?/gi;
  const reserved = /^(where|on|join|left|right|inner|outer|cross|full|group|order|limit|set|values|using|natural|union)$/i;
  for (const m of text.matchAll(re)) {
    const table = Object.keys(schema).find((t) => t.toLowerCase() === m[1].toLowerCase());
    if (!table) continue;
    out.set(table.toLowerCase(), table);
    if (m[2] && !reserved.test(m[2])) out.set(m[2].toLowerCase(), table);
  }
  return out;
}

for (const lang of Object.keys(words)) {
  disposables.push(monaco.languages.registerCompletionItemProvider(lang, {
    triggerCharacters: ['.'],
    provideCompletionItems(model, position) {
      const schema = schemas.get(model.uri.toString()) ?? {};
      const word = model.getWordUntilPosition(position);
      const range = new monaco.Range(position.lineNumber, word.startColumn, position.lineNumber, word.endColumn);
      const K = monaco.languages.CompletionItemKind;
      const before = model.getLineContent(position.lineNumber).slice(0, word.startColumn - 1);

      // "alias." or "table." -> that table's columns only.
      const dot = /(["`]?)(\w+)\1\.$/.exec(before);
      if (dot) {
        const table = aliases(model.getValue(), schema).get(dot[2].toLowerCase());
        const cols = table ? schema[table] : undefined;
        if (cols) {
          return { suggestions: cols.map((c) => ({ label: c, kind: K.Field, insertText: c, detail: table, range })) };
        }
        return { suggestions: [] };
      }

      const suggestions: monaco.languages.CompletionItem[] = [];
      for (const [table, cols] of Object.entries(schema)) {
        suggestions.push({ label: table, kind: K.Struct, insertText: table, detail: 'table', range, sortText: '1' + table });
        for (const c of cols) {
          suggestions.push({ label: c, kind: K.Field, insertText: c, detail: table, range, sortText: '2' + c });
        }
      }
      for (const kw of words[lang].keywords) {
        suggestions.push({ label: kw, kind: K.Keyword, insertText: kw, range, sortText: '3' + kw });
      }
      for (const fn of words[lang].functions) {
        suggestions.push({ label: fn, kind: K.Function, insertText: fn, range, sortText: '4' + fn });
      }
      return { suggestions };
    },
  }));
}
