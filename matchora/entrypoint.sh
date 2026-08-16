#!/bin/sh
set -eu
DATA="/home/matchora/.oraora/matchora"
SEED="/usr/share/matchora/config"
mkdir -p "$DATA/config"
if [ -d "$SEED" ]; then
  for f in "$SEED"/*; do
    [ -f "$f" ] || continue
    dest="$DATA/config/$(basename "$f")"
    [ -f "$dest" ] || cp "$f" "$dest"
  done
fi
chown matchora:matchora "$DATA"
for child in "$DATA"/*; do
  [ -e "$child" ] || continue
  [ -L "$child" ] && continue
  chown -R matchora:matchora "$child"
done
exec runuser -u matchora -- /usr/local/bin/matchora "$@"
