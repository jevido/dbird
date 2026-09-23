-- Sample shop database for trying dbird's SQLite support.
PRAGMA foreign_keys = ON;

CREATE TABLE customers (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    email      TEXT NOT NULL UNIQUE,
    country    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE products (
    id       INTEGER PRIMARY KEY,
    sku      TEXT NOT NULL UNIQUE,
    name     TEXT NOT NULL,
    price    REAL NOT NULL,
    image    BLOB
);

CREATE TABLE orders (
    id          INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES customers (id),
    status      TEXT NOT NULL CHECK (status IN ('pending', 'paid', 'shipped', 'cancelled')),
    total       REAL NOT NULL,
    ordered_at  TEXT NOT NULL
);

WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 5000)
INSERT INTO customers (name, email, country, created_at)
SELECT 'Customer ' || i, 'customer' || i || '@example.com',
       CASE i % 4 WHEN 0 THEN 'NL' WHEN 1 THEN 'BE' WHEN 2 THEN 'DE' ELSE 'FR' END,
       datetime('now', '-' || i || ' hours')
FROM n;

WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 500)
INSERT INTO products (sku, name, price, image)
SELECT printf('SKU-%05d', i), 'Product ' || i, round(5 + (abs(random()) % 49500) / 100.0, 2),
       CASE WHEN i % 10 = 0 THEN randomblob(8) END
FROM n;

WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 20000)
INSERT INTO orders (customer_id, status, total, ordered_at)
SELECT 1 + abs(random()) % 5000,
       CASE i % 7 WHEN 0 THEN 'pending' WHEN 6 THEN 'cancelled' WHEN 1 THEN 'paid' ELSE 'shipped' END,
       round((abs(random()) % 100000) / 100.0, 2),
       datetime('now', '-' || (abs(random()) % 730) || ' days')
FROM n;

CREATE INDEX orders_customer ON orders (customer_id);

CREATE VIEW big_orders AS SELECT * FROM orders WHERE total > 500;
