#!/usr/bin/env bash
# Usage: ./migrate.sh <env> [--apply] [--max N]
# Loads .env.<env> (needs API_URL, API_TOKEN). Dry-run unless --apply.
# Uses the API, not the DB: validation and side effects stay intact.
set -euo pipefail

env_name=${1:?usage: $0 <env> [--apply] [--max N]}; shift
apply=0; max=100
while [ $# -gt 0 ]; do
  case $1 in
    --apply) apply=1 ;;
    --max) max=${2:?--max needs a number}; shift ;;
    *) echo "unknown arg $1" >&2; exit 2 ;;
  esac
  shift
done

envfile=".env.$env_name"
[ -f "$envfile" ] || { echo "missing $envfile" >&2; exit 1; }
set -a; . "./$envfile"; set +a
: "${API_URL:?}" "${API_TOKEN:?}"

mode=DRY-RUN; [ $apply = 1 ] && mode=APPLY
echo "[$mode] env=$env_name host=${API_URL#*://}"

if [ $apply = 1 ] && [ "$env_name" = prod ]; then
  read -r -p "Type 'prod' to apply on production: " ans
  [ "$ans" = prod ] || { echo aborted; exit 1; }
fi

# Select only records still needing the change (idempotent). Adjust the filter.
ids=$(curl -fsS -H "Authorization: Bearer $API_TOKEN" "$API_URL/items?status=old" | jq -r '.[].id')
count=$(printf '%s' "$ids" | grep -c . || true)
echo "selected $count"
[ "$count" -le "$max" ] || { echo "selected $count > --max $max, aborting" >&2; exit 1; }

log="migrate-$env_name-$(date +%Y%m%d%H%M%S).log"
for id in $ids; do
  echo "$id: status old -> new"
  [ $apply = 1 ] || continue
  # Record the old value first so there is a rollback path.
  curl -fsS -H "Authorization: Bearer $API_TOKEN" "$API_URL/items/$id" | jq -c '{id, status}' >> "$log"
  curl -fsS -X PATCH -H "Authorization: Bearer $API_TOKEN" -H 'Content-Type: application/json' \
    -d '{"status":"new"}' "$API_URL/items/$id" >/dev/null || { echo "failed at $id" >&2; exit 1; }
  sleep 0.1
done
[ $apply = 1 ] && echo "done, rollback log: $log" || echo "dry-run only; re-run with --apply"
