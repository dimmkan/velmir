param(
    [Parameter(Mandatory = $true)][string]$BackupName,
    [Parameter(Mandatory = $true)][string]$NewDatabaseName
)

$ErrorActionPreference = "Stop"
if ($BackupName -notmatch '^[A-Za-z0-9._-]+$') {
    throw "Invalid backup name"
}
if ($NewDatabaseName -notmatch '^[A-Za-z0-9_]+$') {
    throw "Database name may contain only letters, digits and underscores"
}

docker compose exec -T admin-db sh -c 'set -eu; test -s "/backups/$1"; createdb --username="$POSTGRES_USER" "$2"; pg_restore --format=custom --exit-on-error --single-transaction --no-owner --no-acl --username="$POSTGRES_USER" --dbname="$2" "/backups/$1"' sh $BackupName $NewDatabaseName
if ($LASTEXITCODE -ne 0) {
    throw "Restore failed; use another fresh database for the next attempt"
}
Write-Output "Restored $BackupName into new admin database $NewDatabaseName"
