# Sample databases

Three sample databases with the same small shop in them (customers, products,
orders, order items), so you can try dbird against every supported driver.

```sh
wails3 task dev:db:up     # start PostgreSQL + MySQL containers, create the SQLite file
wails3 task dev:db:down   # stop them and throw the data away
```

It uses `docker compose` when the Docker daemon is reachable, otherwise
`podman compose`. The first start pulls the images and seeds the data (about
30 seconds). `down` followed by `up` gives you fresh data.

## Connections

Add these in dbird with the **+** next to *Connections*:

| Type | Host | Port | Database | User / password | Other |
| --- | --- | --- | --- | --- | --- |
| PostgreSQL | `127.0.0.1` | `54320` | `shop` | `dbird` / `dbird` | SSL mode `disable` |
| MySQL | `127.0.0.1` | `33061` | `shop` | `dbird` / `dbird` | |
| SQLite | | | `dev/data/shop.sqlite` (use Browse…) | | |

Or paste a URL (tick *Use connection URL / DSN*):

- `postgres://dbird:dbird@127.0.0.1:54320/shop?sslmode=disable`
- `dbird:dbird@tcp(127.0.0.1:33061)/shop`

## What's inside

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| customers | 10,000 (uuid, `text[]` tags, `jsonb` preferences) | 10,000 (`JSON` preferences) | 5,000 |
| products | 1,000 (`numeric`, some `bytea` images) | 1,000 (`ENUM` category, `VARBINARY`) | 500 (some `BLOB`s) |
| orders | 100,000 (`order_status` enum) | ~100,000 | 20,000 |
| order_items | 250,000 | 250,000 | |
| views | `order_totals`, `analytics.monthly_revenue` (materialized) | `order_totals` | `big_orders` |
| other | schema `analytics` with a table named `"Page Views"`; function `customer_lifetime_value(id)` | database `analytics` | |

## A huge schema

`wails3 task dev:db:huge` adds a database `huge` with 300,000 relations (about
a minute). Connect to it with database `huge` to see autocomplete switch to
lookups: *Automatic* notices the size and queries table names by prefix as you
type. Setting the connection to *Load all tables and columns on connect* shows
why that is not the default for big schemas.

`dev/postgres/huge.sql` and `dev/mysql/huge.sql` are also plain scripts you
can paste into a tab and run with `Alt+X`: they create a stored procedure,
call it and drop it again, which shows dbird keeping procedure bodies (with
their inner `;`) together.

## Things to try

- Double-click `orders` in the tree to open its data, sort by a column, press
  `Enter` on a `preferences` cell to see formatted JSON.
- Type `select * from customers c where c.` and watch the column suggestions.
- Revenue per country (PostgreSQL):

  ```sql
  select c.country, count(*) as orders, round(sum(t.total), 2) as revenue
  from order_totals t
  join customers c on c.id = t.customer_id
  group by 1
  order by 3 desc;
  ```

- Session state per tab: run `set search_path to analytics;` then
  `select * from monthly_revenue;` in the same tab, and see that another tab
  still uses `public`.
- Transactions: `begin;`, `update products set stock = 0 where id = 1;`,
  check the row from another tab (unchanged), then `rollback;`.
- Cancel: run `select pg_sleep(30);` and press **Stop**.
- Big results: set the limit to 50,000 and run `select * from order_items;`.
- A script with an error in the middle (`Alt+X`) gives one result tab per statement:

  ```sql
  select count(*) from customers;
  select * from does_not_exist;
  select count(*) from orders;
  ```

## Tests against these databases

The grid-editing tests can run against the sample databases (they use their
own scratch tables and clean up after themselves):

```sh
go test -tags devdb -run DevDB ./internal/dbx/
```
