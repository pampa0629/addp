#!/bin/sh
set -eu
exec sh /addp/redis-authentication.sh business --databases 1 --appendonly yes --save "" \
    --maxmemory 256mb --maxmemory-policy noeviction
