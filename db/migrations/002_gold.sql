-- Synthetic warehouse for "Northwind Cloud", a fictional SaaS with three plans.
-- Values are deterministic: noise comes from md5 of a seed string, never from random().

CREATE SCHEMA gold;

-- noise(seed, n): a stable integer in [0, n)
CREATE FUNCTION pg_temp.noise(seed text, n int) RETURNS int
LANGUAGE sql IMMUTABLE AS $$
  SELECT mod(abs(('x' || substr(md5(seed), 1, 8))::bit(32)::int), n)
$$;

CREATE TEMP TABLE plans (plan text, base_mrr numeric, base_customers int, growth numeric);
INSERT INTO plans VALUES
  ('starter',     18000, 900, 0.010),
  ('team',        52000, 310, 0.014),
  ('enterprise', 118000,  42, 0.018);

CREATE TEMP TABLE days AS
SELECT d::date AS date,
       (extract(year FROM d) - 2024) * 12 + extract(month FROM d) - 1 AS m
FROM generate_series('2024-01-01'::date, '2026-09-30'::date, interval '1 day') AS d;

CREATE TABLE gold.revenue_daily (
  date            date    NOT NULL,
  plan            text    NOT NULL,
  mrr_usd         numeric(12,2) NOT NULL,
  new_mrr_usd     numeric(12,2) NOT NULL,
  churned_mrr_usd numeric(12,2) NOT NULL,
  PRIMARY KEY (date, plan)
);
INSERT INTO gold.revenue_daily
SELECT d.date, p.plan,
       round(p.base_mrr * (1 + p.growth * d.m) * (0.99 + pg_temp.noise('mrr' || d.date || p.plan, 200) / 10000.0), 2),
       round(p.base_mrr * (1 + p.growth * d.m) * (0.0030 + pg_temp.noise('new' || d.date || p.plan, 30) / 10000.0), 2),
       round(p.base_mrr * (1 + p.growth * d.m) * (0.0015 + pg_temp.noise('chu' || d.date || p.plan, 20) / 10000.0), 2)
FROM days d CROSS JOIN plans p;

CREATE TABLE gold.customers_monthly (
  month    date NOT NULL,
  plan     text NOT NULL,
  active   integer NOT NULL,
  new      integer NOT NULL,
  churned  integer NOT NULL,
  PRIMARY KEY (month, plan)
);
INSERT INTO gold.customers_monthly
SELECT date_trunc('month', d.date)::date, p.plan,
       round(p.base_customers * (1 + p.growth * d.m) * (0.99 + pg_temp.noise('act' || d.date || p.plan, 200) / 10000.0)),
       round(p.base_customers * (0.05 + pg_temp.noise('cnew' || d.date || p.plan, 30) / 1000.0)),
       round(p.base_customers * (0.03 + pg_temp.noise('cchu' || d.date || p.plan, 20) / 1000.0))
FROM days d CROSS JOIN plans p
WHERE extract(day FROM d.date) = 1;

CREATE TABLE gold.support_daily (
  date               date NOT NULL PRIMARY KEY,
  tickets            integer NOT NULL,
  first_response_min numeric(6,1) NOT NULL,
  csat               numeric(3,2) NOT NULL
);
INSERT INTO gold.support_daily
SELECT d.date,
       60 + d.m::int + pg_temp.noise('tix' || d.date, 40)
         - CASE WHEN extract(isodow FROM d.date) >= 6 THEN 30 ELSE 0 END,
       round(42 - d.m * 0.6 + pg_temp.noise('frt' || d.date, 150) / 10.0, 1),
       round(4.1 + pg_temp.noise('csat' || d.date, 70) / 100.0, 2)
FROM days d;

CREATE TABLE gold.finance_monthly (
  month            date NOT NULL PRIMARY KEY,
  revenue_usd      numeric(14,2) NOT NULL,
  cost_usd         numeric(14,2) NOT NULL,
  gross_margin_pct numeric(5,1) NOT NULL
);
INSERT INTO gold.finance_monthly
SELECT month, revenue, cost, round((revenue - cost) / revenue * 100, 1)
FROM (
  SELECT r.month, r.revenue,
         round(r.revenue * (0.26 + pg_temp.noise('cost' || r.month, 60) / 1000.0), 2) AS cost
  FROM (
    SELECT date_trunc('month', date)::date AS month, round(sum(mrr_usd) / count(DISTINCT date), 2) AS revenue
    FROM gold.revenue_daily GROUP BY 1
  ) r
) f;

CREATE TABLE gold.infra_daily (
  date     date NOT NULL,
  region   text NOT NULL,
  servers  integer NOT NULL,
  cost_usd numeric(10,2) NOT NULL,
  PRIMARY KEY (date, region)
);
INSERT INTO gold.infra_daily
SELECT d.date, r.region,
       r.base + (d.m / 3)::int + pg_temp.noise('srv' || d.date || r.region, 4),
       round((r.base + (d.m / 3)::int) * r.rate * (0.95 + pg_temp.noise('icost' || d.date || r.region, 100) / 1000.0), 2)
FROM days d
CROSS JOIN (VALUES ('eu-west', 24, 11.5), ('us-east', 30, 10.8), ('ap-south', 9, 12.4)) AS r(region, base, rate);

ANALYZE gold.revenue_daily, gold.customers_monthly, gold.support_daily, gold.finance_monthly, gold.infra_daily;
