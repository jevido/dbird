// Splits a SQL script into statements, tracking source offsets so the editor
// can find and highlight the statement under the cursor.
//
// Statements end at a top-level `;` or at a blank line (like DBeaver's
// "blank line is statement delimiter" default). Quotes, comments and
// PostgreSQL dollar-quoted bodies are respected.

export type Dialect = 'postgres' | 'mysql' | 'sqlite';

export interface Statement {
  text: string;
  from: number; // offset of first character
  to: number; // offset just past last character (excluding the `;`)
}

export function splitStatements(src: string, dialect: Dialect = 'postgres'): Statement[] {
  const out: Statement[] = [];
  let start = 0;
  let hasCode = false;
  let i = 0;
  const n = src.length;

  const push = (end: number) => {
    if (hasCode) {
      let a = start;
      let b = end;
      while (a < b && /\s/.test(src[a])) a++;
      while (b > a && /\s/.test(src[b - 1])) b--;
      out.push({ text: src.slice(a, b), from: a, to: b });
    }
    hasCode = false;
  };

  while (i < n) {
    const c = src[i];
    const next = src[i + 1];

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
      if (m && !/[A-Za-z0-9_]/.test(src[i - 1] ?? '')) {
        hasCode = true;
        const tag = m[0];
        const end = src.indexOf(tag, i + tag.length);
        i = end < 0 ? n : end + tag.length;
        continue;
      }
    }
    if (c === ';') {
      push(i);
      start = i + 1;
      i++;
      continue;
    }
    if (c === '\n') {
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
