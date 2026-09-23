-- Sample shop database for trying dbird. Runs once, on the container's first start.
USE shop;

-- Helper: numbers 0..99999
CREATE TABLE digits (d INT PRIMARY KEY);
INSERT INTO digits VALUES (0), (1), (2), (3), (4), (5), (6), (7), (8), (9);
CREATE TABLE seq (n INT PRIMARY KEY);
INSERT INTO seq
SELECT a.d + b.d * 10 + c.d * 100 + e.d * 1000 + f.d * 10000
FROM digits a, digits b, digits c, digits e, digits f;

CREATE TABLE customers (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    name        VARCHAR(100) NOT NULL,
    email       VARCHAR(200) NOT NULL UNIQUE,
    country     CHAR(2) NOT NULL,
    vip         BOOLEAN NOT NULL DEFAULT FALSE,
    preferences JSON,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE products (
    id       INT AUTO_INCREMENT PRIMARY KEY,
    sku      VARCHAR(20) NOT NULL UNIQUE,
    name     VARCHAR(100) NOT NULL,
    category ENUM('furniture', 'electronics', 'kitchen', 'office', 'garden') NOT NULL,
    price    DECIMAL(10, 2) NOT NULL,
    stock    INT NOT NULL,
    image    VARBINARY(16)
);

CREATE TABLE orders (
    id          BIGINT AUTO_INCREMENT PRIMARY KEY,
    customer_id INT NOT NULL,
    status      ENUM('pending', 'paid', 'shipped', 'cancelled') NOT NULL,
    ordered_at  DATETIME NOT NULL,
    note        TEXT,
    FOREIGN KEY (customer_id) REFERENCES customers (id),
    INDEX (ordered_at)
);

CREATE TABLE order_items (
    order_id   BIGINT NOT NULL,
    product_id INT NOT NULL,
    quantity   INT NOT NULL,
    unit_price DECIMAL(10, 2) NOT NULL,
    PRIMARY KEY (order_id, product_id),
    FOREIGN KEY (order_id) REFERENCES orders (id),
    FOREIGN KEY (product_id) REFERENCES products (id)
);

INSERT INTO customers (name, email, country, vip, preferences, created_at)
SELECT
    CONCAT(ELT(1 + n % 12, 'Anna', 'Bram', 'Chloe', 'Daan', 'Emma', 'Finn', 'Lotte', 'Milan', 'Noor', 'Sem', 'Tess', 'Lucas'), ' ',
           ELT(1 + (n DIV 12) % 10, 'Jansen', 'de Vries', 'Bakker', 'Visser', 'Smit', 'Meijer', 'Mulder', 'Bos', 'Vos', 'Peters')),
    CONCAT('customer', n, '@example.com'),
    ELT(1 + n % 8, 'NL', 'BE', 'DE', 'FR', 'GB', 'US', 'ES', 'SE'),
    n % 7 = 0,
    IF(n % 3 = 0, JSON_OBJECT('theme', 'dark', 'language', 'nl'), NULL),
    NOW() - INTERVAL n HOUR
FROM seq WHERE n BETWEEN 1 AND 10000;

INSERT INTO products (sku, name, category, price, stock, image)
SELECT
    CONCAT('SKU-', LPAD(n, 5, '0')),
    CONCAT(ELT(1 + n % 7, 'Ergonomic', 'Rustic', 'Sleek', 'Refined', 'Handmade', 'Small', 'Tasty'), ' ',
           ELT(1 + n % 8, 'Chair', 'Keyboard', 'Lamp', 'Mug', 'Backpack', 'Notebook', 'Headphones', 'Plant')),
    ELT(1 + n % 5, 'furniture', 'electronics', 'kitchen', 'office', 'garden'),
    ROUND(5 + RAND() * 495, 2),
    FLOOR(RAND() * 500),
    IF(n % 10 = 0, UNHEX(MD5(n)), NULL)
FROM seq WHERE n BETWEEN 1 AND 1000;

INSERT INTO orders (customer_id, status, ordered_at, note)
SELECT
    1 + FLOOR(RAND() * 9999),
    ELT(1 + n % 7, 'pending', 'paid', 'paid', 'shipped', 'shipped', 'shipped', 'cancelled'),
    NOW() - INTERVAL FLOOR(RAND() * 730 * 24) HOUR,
    CASE WHEN n % 50 = 0 THEN 'Please ring twice' WHEN n % 77 = 0 THEN 'Leave at the neighbours' END
FROM seq WHERE n BETWEEN 1 AND 100000;

INSERT IGNORE INTO order_items (order_id, product_id, quantity, unit_price)
SELECT o.id, p.id, 1 + FLOOR(RAND() * 4), p.price
FROM orders o
JOIN digits k ON k.d <= o.id % 4
JOIN products p ON p.id = 1 + (o.id * 7 + k.d * 131) % 1000;

CREATE VIEW order_totals AS
SELECT o.id, o.customer_id, o.status, o.ordered_at, SUM(i.quantity * i.unit_price) AS total
FROM orders o JOIN order_items i ON i.order_id = o.id
GROUP BY o.id, o.customer_id, o.status, o.ordered_at;

-- A second database, visible as another schema in the tree.
CREATE DATABASE analytics;
GRANT ALL PRIVILEGES ON analytics.* TO 'dbird'@'%';
CREATE TABLE analytics.page_views (
    id        BIGINT AUTO_INCREMENT PRIMARY KEY,
    path      VARCHAR(100) NOT NULL,
    viewed_at DATETIME NOT NULL
);
INSERT INTO analytics.page_views (path, viewed_at)
SELECT ELT(1 + n % 5, '/', '/products', '/cart', '/checkout', '/account'), NOW() - INTERVAL n MINUTE
FROM seq WHERE n BETWEEN 1 AND 5000;

-- Drop the helper tables.
DROP TABLE shop.seq, shop.digits;
