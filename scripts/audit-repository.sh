#!/bin/sh
set -eu

if find . -path './.git' -prune -o -name '.env' -print | grep -q .; then
  echo 'local .env file found; ensure it is ignored and not included in delivery' >&2
fi

if rg -n --hidden -g '!.git/**' -g '!scripts/audit-repository.sh' \
  'BEGIN (RSA |OPENSSH |EC )?PRIVATE KEY|AKIA[0-9A-Z]{16}|sk-[A-Za-z0-9]{20,}' .; then
  echo 'possible secret detected' >&2
  exit 1
fi

rg -q '^USER app$' Dockerfile
if rg -n 'TODO|FIXME|panic\(' internal cmd; then
  echo 'unfinished marker found in runtime code' >&2
  exit 1
fi

echo 'Repository audit passed'
