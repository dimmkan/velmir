#!/usr/bin/env sh
set -eu
export MSYS_NO_PATHCONV=1

if [ "$#" -ne 2 ]; then
  echo "Usage: sh scripts/restore-admin-fresh.sh BACKUP_NAME NEW_DATABASE_NAME" >&2
  exit 2
fi
case "$1" in
  *[!A-Za-z0-9._-]*) echo "Invalid backup name" >&2; exit 2 ;;
esac
case "$2" in
  *[!A-Za-z0-9_]*|'') echo "Database name may contain only letters, digits and underscores" >&2; exit 2 ;;
esac

docker compose exec -T admin-db sh -c '
  set -eu
  test -s "/backups/$1"
  createdb --username="$POSTGRES_USER" "$2"
  pg_restore --format=custom --exit-on-error --single-transaction --no-owner --no-acl --username="$POSTGRES_USER" --dbname="$2" "/backups/$1"
' sh "$1" "$2"
echo "Restored $1 into new admin database $2"
