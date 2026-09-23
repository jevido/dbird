import { describe, expect, it } from 'vitest';
import { splitStatements, statementAt, unfilteredDelete } from './sqlsplit';

const texts = (src: string, d: 'postgres' | 'mysql' | 'sqlite' = 'postgres') => splitStatements(src, d).map((s) => s.text);

describe('splitStatements', () => {
  it('splits on semicolons and blank lines', () => {
    expect(texts('select 1; select 2;')).toEqual(['select 1', 'select 2']);
    expect(texts('select 1\n\nselect 2')).toEqual(['select 1', 'select 2']);
  });
  it('respects strings, comments and dollar quotes', () => {
    expect(texts("select ';' as x;\n-- c;\nselect 2")).toEqual(["select ';' as x", '-- c;\nselect 2']);
    expect(texts("select 'it''s; ok'; /* ; */ select 3")).toEqual(["select 'it''s; ok'", '/* ; */ select 3']);
    expect(texts('create function f() returns int as $$\nbegin\n\n return 1;\nend $$ language plpgsql;\nselect f()')).toHaveLength(2);
    expect(texts("select 'a\\';b'; select 4", 'mysql')).toEqual(["select 'a\\';b'", 'select 4']);
  });
  it('skips comment-only statements', () => {
    expect(texts('-- only comment\n;\n')).toEqual([]);
  });
  it('finds the statement at the cursor', () => {
    const st = splitStatements('select 1;\nselect 2;\n\n');
    expect(statementAt(st, 0)?.text).toBe('select 1');
    expect(statementAt(st, 9)?.text).toBe('select 1');
    expect(statementAt(st, 12)?.text).toBe('select 2');
  });
});

describe('routine bodies', () => {
  it('keeps a MySQL procedure body together, blank lines and all', () => {
    const src = `DROP PROCEDURE IF EXISTS p;
CREATE PROCEDURE p(total INT)
BEGIN
  DECLARE i INT DEFAULT 1;

  WHILE i <= total DO
    IF i % 2 = 0 THEN
      SET @x = CASE WHEN i > 10 THEN 'big' ELSE 'small' END;
    END IF;
    SET i = i + 1;
  END WHILE;
END;
CALL p(3);`;
    const st = texts(src, 'mysql');
    expect(st).toHaveLength(3);
    expect(st[1].startsWith('CREATE PROCEDURE p(total INT)')).toBe(true);
    expect(st[1].endsWith('END WHILE;\nEND')).toBe(true);
    expect(st[2]).toBe('CALL p(3)');
  });
  it('handles triggers, functions, labels and DEFINER', () => {
    const src = `CREATE DEFINER=\`root\`@\`%\` TRIGGER t BEFORE INSERT ON x FOR EACH ROW
BEGIN
  SET NEW.a = 1;
END;
CREATE FUNCTION f() RETURNS INT DETERMINISTIC RETURN 1;
CREATE PROCEDURE q() lbl: BEGIN LEAVE lbl; END;
select 1`;
    expect(texts(src, 'mysql')).toHaveLength(4);
  });
  it('supports DELIMITER like the mysql client', () => {
    const src = `DELIMITER //
CREATE PROCEDURE p()
BEGIN
  SELECT 1;
  SELECT 2;
END //
DELIMITER ;
CALL p();

DELIMITER $$
CREATE FUNCTION f() RETURNS INT DETERMINISTIC BEGIN RETURN 1; END$$
DELIMITER ;
select f();`;
    const st = texts(src, 'mysql');
    expect(st).toEqual([
      'CREATE PROCEDURE p()\nBEGIN\n  SELECT 1;\n  SELECT 2;\nEND',
      'CALL p()',
      'CREATE FUNCTION f() RETURNS INT DETERMINISTIC BEGIN RETURN 1; END',
      'select f()',
    ]);
  });
  it('does not treat a transaction BEGIN or a column named function as a body', () => {
    expect(texts('begin;\nupdate t set a = 1;\ncommit;', 'mysql')).toEqual(['begin', 'update t set a = 1', 'commit']);
    expect(texts('create table t (function int);\nselect 1', 'mysql')).toEqual(['create table t (function int)', 'select 1']);
  });
  it('keeps PostgreSQL BEGIN ATOMIC bodies and $$ procedures together', () => {
    const atomic = `CREATE FUNCTION f(a int) RETURNS int LANGUAGE sql
BEGIN ATOMIC
  SELECT a + 1;
END;
select f(1)`;
    expect(texts(atomic)).toHaveLength(2);
    const huge = `CREATE PROCEDURE make_relations(total int) LANGUAGE plpgsql AS $$
BEGIN
  FOR i IN 1..total LOOP
    IF i % 1000 = 0 THEN
      COMMIT;
    END IF;
  END LOOP;
END;
$$;
CALL make_relations(300000);
DROP PROCEDURE make_relations(int);`;
    expect(texts(huge)).toEqual([huge.split('$$;')[0] + '$$', 'CALL make_relations(300000)', 'DROP PROCEDURE make_relations(int)']);
  });
});

describe('unfilteredDelete', () => {
  it('flags deletes without WHERE', () => {
    expect(unfilteredDelete('delete from customers')).toBe('customers');
    expect(unfilteredDelete('DELETE FROM public.orders;')).toBe('public.orders');
    expect(unfilteredDelete('delete from analytics."Page Views"')).toBe('analytics."Page Views"');
    expect(unfilteredDelete('delete from only orders')).toBe('orders');
    expect(unfilteredDelete('delete orders from orders join customers c on c.id = orders.customer_id', 'mysql')).toBe('orders');
    expect(unfilteredDelete('  -- tidy up\n  delete from t')).toBe('t');
  });
  it('ignores WHERE that is not a top-level clause', () => {
    expect(unfilteredDelete("delete from t -- where id = 1")).toBe('t');
    expect(unfilteredDelete("delete from t /* where */")).toBe('t');
    expect(unfilteredDelete('delete from t using (select id from u where x) s')).toBe('t');
    expect(unfilteredDelete('delete from "where"')).toBe('"where"');
  });
  it('allows filtered deletes and other statements', () => {
    expect(unfilteredDelete('delete from customers where id = 1')).toBeNull();
    expect(unfilteredDelete('delete from t where id in (select id from u)')).toBeNull();
    expect(unfilteredDelete('with old as (select id from t where x) delete from t where id in (select id from old)')).toBeNull();
    expect(unfilteredDelete("select 'delete from t'")).toBeNull();
    expect(unfilteredDelete('update t set deleted = true')).toBeNull();
    expect(unfilteredDelete('truncate t')).toBeNull();
    expect(unfilteredDelete('alter table o add foreign key (c) references c (id) on delete cascade')).toBeNull();
    expect(unfilteredDelete('explain delete from t')).toBeNull();
  });
  it('flags a CTE followed by an unfiltered delete', () => {
    expect(unfilteredDelete('with x as (select 1 where true) delete from t')).toBe('t');
  });
});
