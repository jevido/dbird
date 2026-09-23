-- Sample shop database for trying dbird. Runs once, on the container's first start.

CREATE TYPE order_status AS ENUM ('pending', 'paid', 'shipped', 'cancelled');

CREATE TABLE customers (
    id          serial PRIMARY KEY,
    uid         uuid NOT NULL DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    email       text NOT NULL UNIQUE,
    country     text NOT NULL,
    tags        text[] NOT NULL DEFAULT '{}',
    preferences jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE products (
    id       serial PRIMARY KEY,
    sku      text NOT NULL UNIQUE,
    name     text NOT NULL,
    category text NOT NULL,
    price    numeric(10, 2) NOT NULL,
    stock    integer NOT NULL,
    image    bytea
);

CREATE TABLE orders (
    id          bigserial PRIMARY KEY,
    customer_id integer NOT NULL REFERENCES customers (id),
    status      order_status NOT NULL,
    ordered_at  timestamptz NOT NULL,
    note        text
);

CREATE TABLE order_items (
    order_id   bigint NOT NULL REFERENCES orders (id),
    product_id integer NOT NULL REFERENCES products (id),
    quantity   integer NOT NULL,
    unit_price numeric(10, 2) NOT NULL,
    PRIMARY KEY (order_id, product_id)
);

INSERT INTO customers (name, email, country, tags, preferences, created_at)
SELECT
    initcap(first) || ' ' || initcap(last),
    lower(first || '.' || last || i) || '@example.com',
    (ARRAY['NL', 'BE', 'DE', 'FR', 'GB', 'US', 'ES', 'SE'])[1 + i % 8],
    CASE WHEN i % 7 = 0 THEN ARRAY['vip'] WHEN i % 5 = 0 THEN ARRAY['newsletter', 'b2b'] ELSE '{}' END,
    CASE WHEN i % 3 = 0 THEN jsonb_build_object('theme', 'dark', 'language', 'nl', 'notifications', jsonb_build_object('email', true, 'sms', i % 2 = 0)) END,
    now() - (i || ' hours')::interval
FROM generate_series(1, 10000) AS i,
     LATERAL (SELECT (ARRAY['anna', 'bram', 'chloe', 'daan', 'emma', 'finn', 'lotte', 'milan', 'noor', 'sem', 'tess', 'lucas'])[1 + i % 12] AS first,
                     (ARRAY['jansen', 'de vries', 'bakker', 'visser', 'smit', 'meijer', 'mulder', 'bos', 'vos', 'peters'])[1 + (i / 12) % 10] AS last) n;

INSERT INTO products (sku, name, category, price, stock, image)
SELECT
    'SKU-' || lpad(i::text, 5, '0'),
    (ARRAY['Ergonomic', 'Rustic', 'Sleek', 'Refined', 'Handmade', 'Small', 'Tasty'])[1 + i % 7] || ' ' ||
    (ARRAY['Chair', 'Keyboard', 'Lamp', 'Mug', 'Backpack', 'Notebook', 'Headphones', 'Plant'])[1 + i % 8],
    (ARRAY['furniture', 'electronics', 'kitchen', 'office', 'garden'])[1 + i % 5],
    round((5 + random() * 495)::numeric, 2),
    (random() * 500)::int,
    CASE WHEN i % 10 = 0 THEN decode(md5(i::text), 'hex') END
FROM generate_series(1, 1000) AS i;

INSERT INTO orders (customer_id, status, ordered_at, note)
SELECT
    1 + (random() * 9999)::int,
    (ARRAY['pending', 'paid', 'paid', 'shipped', 'shipped', 'shipped', 'cancelled']::order_status[])[1 + i % 7],
    now() - make_interval(secs => random() * 730 * 86400),
    CASE WHEN i % 50 = 0 THEN 'Please ring twice' WHEN i % 77 = 0 THEN 'Leave at the neighbours' END
FROM generate_series(1, 100000) AS i;

INSERT INTO order_items (order_id, product_id, quantity, unit_price)
SELECT DISTINCT ON (o.id, p.product_id)
    o.id, p.product_id, 1 + (random() * 4)::int, pr.price
FROM orders o
CROSS JOIN LATERAL (SELECT 1 + ((o.id * 7 + k * 131) % 1000)::int AS product_id FROM generate_series(1, 1 + (o.id % 4)::int) k) p
JOIN products pr ON pr.id = p.product_id;

CREATE INDEX ON orders (customer_id);
CREATE INDEX ON orders (ordered_at);

CREATE VIEW order_totals AS
SELECT o.id, o.customer_id, o.status, o.ordered_at, sum(i.quantity * i.unit_price) AS total
FROM orders o JOIN order_items i ON i.order_id = o.id
GROUP BY o.id;

-- A second schema, to try the schema tree and qualified names.
CREATE SCHEMA analytics;

CREATE MATERIALIZED VIEW analytics.monthly_revenue AS
SELECT date_trunc('month', ordered_at) AS month, count(*) AS orders, sum(total) AS revenue
FROM order_totals WHERE status <> 'cancelled'
GROUP BY 1 ORDER BY 1;

CREATE TABLE analytics."Page Views" (
    id        bigserial PRIMARY KEY,
    "Path"    text NOT NULL,
    viewed_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO analytics."Page Views" ("Path", viewed_at)
SELECT (ARRAY['/', '/products', '/cart', '/checkout', '/account'])[1 + i % 5], now() - (i || ' minutes')::interval
FROM generate_series(1, 5000) AS i;

-- A function with a dollar-quoted body (the script splitter must not break it up).
CREATE FUNCTION customer_lifetime_value(cid integer) RETURNS numeric
LANGUAGE plpgsql AS $$
DECLARE
    v numeric;
BEGIN
    SELECT coalesce(sum(total), 0) INTO v FROM order_totals WHERE customer_id = cid AND status <> 'cancelled';
    RETURN v;
END;
$$;

ANALYZE;
