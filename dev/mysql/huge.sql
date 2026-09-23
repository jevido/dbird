-- The MySQL counterpart of dev/postgres/huge.sql: creates many views through
-- a stored procedure. Paste it into a dbird tab on a MySQL connection and run
-- it as a script (Alt+X). MySQL is much slower at DDL than PostgreSQL (each
-- view is synced to disk), so this makes 1,000, which takes a few minutes.
DROP PROCEDURE IF EXISTS make_relations;
CREATE PROCEDURE make_relations(total INT)
BEGIN
  DECLARE i INT DEFAULT 1;

  WHILE i <= total DO
    SET @stmt = CONCAT(
      'CREATE OR REPLACE VIEW ',
      ELT(1 + i % 8, 'account', 'invoice', 'shipment', 'customer', 'order', 'event', 'audit', 'metric'),
      '_', LPAD(i, 6, '0'),
      ' AS SELECT ', i, ' AS id, ''row ', i, ''' AS name, NOW() AS created_at, 0.00 AS amount');
    PREPARE s FROM @stmt;
    EXECUTE s;
    DEALLOCATE PREPARE s;
    SET i = i + 1;
  END WHILE;
END;
CALL make_relations(1000);
DROP PROCEDURE make_relations;
