-- A database with 300,000 relations, to feel autocomplete on a huge schema.
-- Run with: wails3 task dev:db:huge   (takes a few minutes)
-- They are views so Postgres doesn't create 300k data files; the catalog
-- (and therefore dbird's lookups) treats them the same as tables.
CREATE PROCEDURE make_relations(total int) LANGUAGE plpgsql AS $$
BEGIN
  FOR i IN 1..total LOOP
    EXECUTE format(
      'CREATE VIEW %I AS SELECT %s::int AS id, %L::text AS name, now() AS created_at, 0::numeric(10,2) AS amount',
      (ARRAY['account', 'invoice', 'shipment', 'customer', 'order', 'event', 'audit', 'metric'])[1 + i % 8] || '_' || lpad(i::text, 6, '0'),
      i, 'row ' || i);
    IF i % 1000 = 0 THEN
      COMMIT; -- keeps the lock table small
    END IF;
  END LOOP;
END;
$$;
CALL make_relations(300000);
DROP PROCEDURE make_relations(int);
