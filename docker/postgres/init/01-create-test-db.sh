#!/bin/bash
set -e

: "${POSTGRES_USER:?POSTGRES_USER is required}"
: "${POSTGRES_TEST_DB:?POSTGRES_TEST_DB is required}"

psql \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set ON_ERROR_STOP=1 \
  --command "CREATE DATABASE \"$POSTGRES_TEST_DB\" OWNER \"$POSTGRES_USER\";"