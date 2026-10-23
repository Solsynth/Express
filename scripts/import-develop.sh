#!/usr/bin/env bash
# Import the legacy Develop tables into the developer portal.
#
# Moves developers, projects, custom apps (with their secrets and board
# widgets), bot accounts (with their chat configs), mini apps, and app products
# into DistributionCenter's schema, so a deployment can stop running the
# Develop service without losing anything.
#
# Safe to re-run; idempotent; never truncates the target. Rows the target
# already holds are skipped, not rewritten. Run it with PGDRY=1 first against a
# real source so you see exactly what will land before any write happens.
#
# Usage:
#   # Import every portal module, in dependency order:
#   ./scripts/import-develop.sh
#
#   # Dry run first (reads, validates, writes nothing):
#   PGDRY=1 ./scripts/import-develop.sh
#
#   # Import one module only:
#   ./scripts/import-develop.sh --module app.solian.develop.apps
#
#   # Override the databases or pass extra flags:
#   SOURCE_DB=dyson_develop TARGET_DB=dyson_distribution \
#     ./scripts/import-develop.sh --batch-size 200
#
# Expect skipped rows and read them:
#   * a child whose parent did not import (a project without its developer, an
#     app without its project, a secret or widget without its app, a chat config
#     without its bot, a product state without its product) is skipped, because
#     importing it would either dangle or be rejected by the foreign key;
#   * a row the target already holds is skipped, so a re-run is cheap.
# Skipped rows are counted per table. They are never overwritten or guessed at.
#
# OAuth clients: if you are also importing into Stargate, run
# ../Stargate/scripts/import-oauth-apps.sh as well. That script owns the
# OAuth/OIDC client registry; this one keeps custom apps in the portal.
#
# Environment:
#   PGHOST         (default: essential01.latxa-bushi.ts.net)
#   PGPORT         (default: 5432)
#   PGUSER         (default: snchan)
#   PGPASSWORD     required; read from env, never echoed
#   PGSSLMODE      (default: disable)
#   SOURCE_DB      (default: dyson_develop)
#   TARGET_DB      (default: dyson_distribution)
#   PGDRY=1        shortcut for --dry-run

set -euo pipefail

PGHOST="${PGHOST:-essential01.latxa-bushi.ts.net}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-snchan}"
PGSSLMODE="${PGSSLMODE:-disable}"

if [ -z "${PGPASSWORD:-}" ]; then
  echo "error: PGPASSWORD is required (read from env, never echoed)" >&2
  exit 1
fi
export PGPASSWORD

SOURCE_DB="${SOURCE_DB:-dyson_develop}"
TARGET_DB="${TARGET_DB:-dyson_distribution}"

DSN_BASE="host=${PGHOST} port=${PGPORT} user=${PGUSER} password=${PGPASSWORD} sslmode=${PGSSLMODE}"
SOURCE_DSN="${DSN_BASE} dbname=${SOURCE_DB}"
TARGET_DSN="${DSN_BASE} dbname=${TARGET_DB}"

# Drop PGPASSWORD from the env once the DSNs are built, so no child process
# inherits it by accident.
unset PGPASSWORD

cd "$(dirname "$0")/.."

IMPORTER_BIN="$(mktemp)"
trap 'rm -f "$IMPORTER_BIN"' EXIT
go build -o "$IMPORTER_BIN" ./cmd/distribution-import

echo "==> source: ${PGUSER}@${PGHOST}:${PGPORT}/${SOURCE_DB} (Develop, password from env)"
echo "==> target: ${PGUSER}@${PGHOST}:${PGPORT}/${TARGET_DB} (DistributionCenter)"
echo

# Default to every portal module, which the importer walks in dependency order
# (developers -> apps -> bots/miniapps -> products).
has_module=0
for arg in "$@"; do
  if [ "$arg" = "--module" ]; then has_module=1; break; fi
done
ARGS=("--source-dsn" "$SOURCE_DSN" "--target-dsn" "$TARGET_DSN")
if [ "$has_module" = "0" ]; then
  ARGS+=("--module" "all")
fi
if [ "${PGDRY:-0}" = "1" ]; then
  ARGS+=("--dry-run")
fi
# Forward any extra flags from the caller (e.g. --batch-size 200).
ARGS+=("$@")

exec "$IMPORTER_BIN" "${ARGS[@]}"
