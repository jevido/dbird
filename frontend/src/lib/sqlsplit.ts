// Splits a SQL script into statements, tracking source offsets so the editor
// can find and highlight the statement under the cursor.
//
// Statements end at a top-level `;` or at a blank line (like DBeaver's
// "blank line is statement delimiter" default). Quotes, comments and
// PostgreSQL dollar-quoted bodies are respected, and so are routine bodies:
//   - MySQL: CREATE PROCEDURE / FUNCTION / TRIGGER / EVENT … BEGIN … END
//     stays one statement, including the `;`s and blank lines inside.
//   - MySQL: `DELIMITER //` lines switch the statement delimiter, as in the
//     mysql command-line client. The DELIMITER lines themselves aren't sent.
//   - PostgreSQL: CREATE FUNCTION / PROCEDURE … BEGIN ATOMIC … END.

export type Dialect = 'postgres' | 'mysql' | 'sqlite';

export interface Statement {
  text: string;
  from: number; // offset of first character
  to: number; // offset just past last character (excluding the delimiter)
}

// "CREATE [OR REPLACE] [DEFINER = x] [AGGREGATE|CONSTRAINT] PROCEDURE|FUNCTION|TRIGGER|EVENT"
const routineHeader =
  /^CREATE\s+(OR\s+REPLACE\s+)?(DEFINER\s*=\s*\S+\s+)?((AGGREGATE|CONSTRAINT)\s+)?(PROCEDURE|FUNCTION|TRIGGER|EVENT)$/i;

const isWordStart = (c: string | undefined) => !!c && /[A-Za-z_]/.test(c);
const isWordChar = (c: string | undefined) => !!c && /[A-Za-z0-9_$]/.test(c);

export function splitStatements(src: string, dialect: Dialect = 'postgres'): Statement[] {
  const out: Statement[] = [];
  const n = src.length;
  let start = 0;
  let hasCode = false;
  let i = 0;
  let delimiter = ';';

  // Per-statement state for routine bodies.
  let words: string[] = []; // first few top-level words of the statement
  let routine = false; // statement creates a routine that may have a body
  let depth = 0; // BEGIN/CASE … END nesting inside the body

  const push = (end: number) => {
    if (hasCode) {
      let a = start;
      let b = end;
      while (a < b && /\s/.test(src[a])) a++;
      while (b > a && /\s/.test(src[b - 1])) b--;
      out.push({ text: src.slice(a, b), from: a, to: b });
    }
    hasCode = false;
    words = [];
    routine = false;
    depth = 0;
  };

  // The word following position j (skipping whitespace), upper-cased.
  const nextWord = (j: number) => /^\s*([A-Za-z_]\w*)/.exec(src.slice(j, j + 64))?.[1].toUpperCase() ?? '';

  while (i < n) {
    const c = src[i];
    const next = src[i + 1];

    // DELIMITER directive (MySQL), only at the start of a statement.
    if (dialect === 'mysql' && !hasCode && (c === 'D' || c === 'd')) {
      const m = /^DELIMITER[ \t]+(\S+)[ \t]*(?=\r?\n|$)/i.exec(src.slice(i, i + 64));
      if (m && /(^|\n)[ \t]*$/.test(src.slice(Math.max(0, i - 64), i))) {
        delimiter = m[1];
        i += m[0].length;
        start = i;
        continue;
      }
    }
    // Line comments.
    if ((c === '-' && next === '-') || (c === '#' && dialect === 'mysql')) {
      const nl = src.indexOf('\n', i);
      i = nl < 0 ? n : nl;
      continue;
    }
    // Block comments.
    if (c === '/' && next === '*') {
      const end = src.indexOf('*/', i + 2);
      i = end < 0 ? n : end + 2;
      continue;
    }
    // Quoted strings and identifiers.
    if (c === "'" || c === '"' || c === '`') {
      hasCode = true;
      const backslash = dialect === 'mysql' && c !== '`';
      i++;
      while (i < n) {
        if (backslash && src[i] === '\\') {
          i += 2;
          continue;
        }
        if (src[i] === c) {
          if (src[i + 1] === c) {
            i += 2; // doubled quote escape
            continue;
          }
          break;
        }
        i++;
      }
      i++;
      continue;
    }
    // PostgreSQL dollar quoting: $$...$$ or $tag$...$tag$.
    if (c === '$' && dialect === 'postgres') {
      const m = /^\$([A-Za-z_][A-Za-z0-9_]*)?\$/.exec(src.slice(i, i + 64));
      if (m && !isWordChar(src[i - 1])) {
        hasCode = true;
        const tag = m[0];
        const end = src.indexOf(tag, i + tag.length);
        i = end < 0 ? n : end + tag.length;
        continue;
      }
    }
    // Custom delimiter (after DELIMITER): ends statements anywhere at top level.
    if (delimiter !== ';' && src.startsWith(delimiter, i)) {
      push(i);
      i += delimiter.length;
      start = i;
      continue;
    }
    // Words: track routine bodies.
    if (isWordStart(c) && !isWordChar(src[i - 1])) {
      let j = i + 1;
      while (j < n && isWordChar(src[j]) && !(delimiter !== ';' && src.startsWith(delimiter, j))) j++;
      const w = src.slice(i, j).toUpperCase();
      hasCode = true;
      if (words.length < 12) {
        words.push(w);
        if (!routine && words[0] === 'CREATE' && ['PROCEDURE', 'FUNCTION', 'TRIGGER', 'EVENT'].includes(w)) {
          routine = routineHeader.test(src.slice(start, j).replace(/^(\s|--[^\n]*\n|\/\*[\s\S]*?\*\/)*/, ''));
        }
      }
      if (routine && dialect !== 'sqlite') {
        const opens =
          dialect === 'mysql'
            ? w === 'BEGIN' || (w === 'CASE' && depth > 0)
            : (w === 'BEGIN' && nextWord(j) === 'ATOMIC') || (w === 'CASE' && depth > 0);
        if (opens) depth++;
        else if (w === 'END' && depth > 0 && !['IF', 'LOOP', 'WHILE', 'REPEAT'].includes(nextWord(j))) depth--;
      }
      i = j;
      continue;
    }
    if (c === ';' && delimiter === ';' && depth === 0) {
      push(i);
      start = i + 1;
      i++;
      continue;
    }
    if (c === '\n' && delimiter === ';' && depth === 0 && !routine) {
      // Blank line: newline, optional horizontal whitespace, newline.
      let j = i + 1;
      while (j < n && (src[j] === ' ' || src[j] === '\t' || src[j] === '\r')) j++;
      if (j < n && src[j] === '\n') {
        push(i);
        start = j;
        i = j;
        continue;
      }
    }
    if (!/\s/.test(c)) hasCode = true;
    i++;
  }
  push(n);
  return out;
}

// Returns the statement the cursor is in, or the closest one before it.
export function statementAt(stmts: Statement[], pos: number): Statement | undefined {
  let best: Statement | undefined;
  for (const s of stmts) {
    if (s.from <= pos) best = s;
    // Cursor right after `;` still counts as "in" the statement.
    if (pos >= s.from && pos <= s.to + 1) return s;
  }
  return best ?? stmts[0];
}

// Replaces comments, string literals and quoted identifiers with spaces, so
// keyword checks can't be fooled by text like 'where' inside a string.
// The result has the same length as the input, so offsets still line up.
export function maskSQL(src: string, dialect: Dialect = 'postgres'): string {
  const out = src.split('');
  const blank = (from: number, to: number) => {
    for (let k = from; k < to && k < out.length; k++) if (out[k] !== '\n') out[k] = ' ';
  };
  let i = 0;
  const n = src.length;
  while (i < n) {
    const c = src[i];
    const next = src[i + 1];
    if ((c === '-' && next === '-') || (c === '#' && dialect === 'mysql')) {
      const nl = src.indexOf('\n', i);
      const end = nl < 0 ? n : nl;
      blank(i, end);
      i = end;
    } else if (c === '/' && next === '*') {
      const e = src.indexOf('*/', i + 2);
      const end = e < 0 ? n : e + 2;
      blank(i, end);
      i = end;
    } else if (c === "'" || c === '"' || c === '`') {
      const backslash = dialect === 'mysql' && c !== '`';
      let j = i + 1;
      while (j < n) {
        if (backslash && src[j] === '\\') {
          j += 2;
          continue;
        }
        if (src[j] === c) {
          if (src[j + 1] === c) {
            j += 2;
            continue;
          }
          break;
        }
        j++;
      }
      blank(i, j + 1);
      i = j + 1;
    } else if (c === '$' && dialect === 'postgres') {
      const m = /^\$([A-Za-z_][A-Za-z0-9_]*)?\$/.exec(src.slice(i, i + 64));
      if (m && !/[A-Za-z0-9_]/.test(src[i - 1] ?? '')) {
        const e = src.indexOf(m[0], i + m[0].length);
        const end = e < 0 ? n : e + m[0].length;
        blank(i, end);
        i = end;
      } else i++;
    } else i++;
  }
  return out.join('');
}

// Returns the table name when stmt is a DELETE without a top-level WHERE
// (i.e. it deletes every row), otherwise null. DELETE must be the statement's
// verb (first keyword, or the main statement after a WITH clause), so
// "ON DELETE CASCADE" doesn't count; WHERE inside subqueries doesn't either.
export function unfilteredDelete(stmt: string, dialect: Dialect = 'postgres'): string | null {
  const masked = maskSQL(stmt, dialect);
  const verbs = new Set(['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'MERGE', 'VALUES', 'TABLE']);
  let depth = 0;
  let first = '';
  let deleteAt = -1;
  let decided = false;
  let hasWhere = false;
  for (const m of masked.matchAll(/[()]|[A-Za-z_][A-Za-z0-9_$]*/g)) {
    const tok = m[0];
    if (tok === '(') depth++;
    else if (tok === ')') depth = Math.max(0, depth - 1);
    else if (depth === 0) {
      const kw = tok.toUpperCase();
      if (!first) {
        first = kw;
        if (kw !== 'DELETE' && kw !== 'WITH') return null;
      }
      if (!decided && verbs.has(kw)) {
        decided = true;
        if (kw !== 'DELETE') return null;
        deleteAt = m.index!;
      } else if (kw === 'WHERE' && deleteAt >= 0) {
        hasWhere = true;
      }
    }
  }
  if (deleteAt < 0 || hasWhere) return null;
  // Table name from the original text (quoted names were masked).
  const rest = stmt.slice(deleteAt + 'delete'.length);
  const t = /^\s+(?:from\s+)?(?:only\s+)?((?:[\w$]+|"[^"]+"|`[^`]+`|\[[^\]]+\])(?:\.(?:[\w$]+|"[^"]+"|`[^`]+`|\[[^\]]+\]))*)/i.exec(rest);
  return t ? t[1] : 'the table';
}
