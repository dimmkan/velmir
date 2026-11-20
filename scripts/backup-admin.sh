#!/usr/bin/env sh
set -eu
export MSYS_NO_PATHCONV=1

backup_name="${1:-velmir-admin-$(date -u +%Y%m%dT%H%M%SZ).dump}"
case "$backup_name" in
  *[!A-Za-z0-9._-]*)
    echo "Backup name may contain only letters, digits, dots, underscores and hyphens" >&2
    exit 2
    ;;
esac

docker compose exec -T admin-db sh -c '
  set -eu
  pg_dump --format=custom --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" --file="/backups/$1"
  test -s "/backups/$1"
' sh "$backup_name"
echo "Created admin database backup in the admin-db-backups volume: $backup_name"
