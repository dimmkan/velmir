param(
    [string]$BackupName = "velmir-admin-$((Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ')).dump"
)

$ErrorActionPreference = "Stop"
if ($BackupName -notmatch '^[A-Za-z0-9._-]+$') {
    throw "Backup name may contain only letters, digits, dots, underscores and hyphens"
}

docker compose exec -T admin-db sh -c 'set -eu; pg_dump --format=custom --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" --file="/backups/$1"; test -s "/backups/$1"' sh $BackupName
if ($LASTEXITCODE -ne 0) {
    throw "Admin database backup failed"
}
Write-Output "Created admin database backup in the admin-db-backups volume: $BackupName"
