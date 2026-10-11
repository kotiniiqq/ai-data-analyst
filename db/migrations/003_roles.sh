#!/bin/sh
# Read-only role the warehouse provider connects as: SELECT on schema gold only, 10 s per statement.
set -e
psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -v pw="${WAREHOUSE_PASSWORD:?WAREHOUSE_PASSWORD is required}" <<'SQL'
CREATE ROLE analyst_ro LOGIN PASSWORD :'pw';
ALTER ROLE analyst_ro SET statement_timeout = '10s';
REVOKE ALL ON SCHEMA public FROM analyst_ro;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM analyst_ro;
GRANT USAGE ON SCHEMA gold TO analyst_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA gold TO analyst_ro;
SQL
