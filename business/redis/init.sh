#!/bin/sh
set -eu
export REDISCLI_AUTH="${BUSINESS_REDIS_ADMIN_PASSWORD:?Business Redis admin password required}"
redis-cli --user addp_business_admin -e --eval /addp/redis/samples.lua
