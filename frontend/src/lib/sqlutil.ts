// Quotes an identifier only when needed, so generated SQL stays readable.
export function quoteIdent(driver: string, name: string): string {
  const plain = /^[a-z_][a-z0-9_$]*$/.test(name) && !reserved.has(name.toUpperCase());
  if (driver === 'mysql') {
    return /^[A-Za-z_][A-Za-z0-9_$]*$/.test(name) && !reserved.has(name.toUpperCase())
      ? name
      : '`' + name.replace(/`/g, '``') + '`';
  }
  return plain ? name : '"' + name.replace(/"/g, '""') + '"';
}

const reserved = new Set(
  (
    'ALL ANALYSE ANALYZE AND ANY ARRAY AS ASC ASYMMETRIC BOTH CASE CAST CHECK COLLATE COLUMN CONSTRAINT CREATE ' +
    'CURRENT_DATE CURRENT_ROLE CURRENT_TIME CURRENT_TIMESTAMP CURRENT_USER DEFAULT DEFERRABLE DESC DISTINCT DO ' +
    'ELSE END EXCEPT FALSE FETCH FOR FOREIGN FROM GRANT GROUP HAVING IN INITIALLY INTERSECT INTO KEY LATERAL LEADING ' +
    'LIMIT LOCALTIME LOCALTIMESTAMP NOT NULL OFFSET ON ONLY OR ORDER PLACING PRIMARY REFERENCES RETURNING SELECT ' +
    'SESSION_USER SOME SYMMETRIC TABLE THEN TO TRAILING TRUE UNION UNIQUE USER USING VARIADIC WHEN WHERE WINDOW WITH ' +
    'INDEX RANGE ROWS VALUES'
  ).split(' '),
);
