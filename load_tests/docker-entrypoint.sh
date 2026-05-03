#!/bin/sh
#
# docker-entrypoint.sh - Resolve token fallback before starting any process
#
# LOADTEST_ADMIN_TOKEN is required by all containers.
# If not explicitly set, fall back to ADMIN_SEED_TOKEN from the project .env
# (which is loaded via env_file: ../.env in docker-compose.loadtest.yml).
#
# This avoids complex shell-in-YAML escaping: the entrypoint runs as PID 1
# and resolves env vars BEFORE Docker Compose command substitution kicks in.

set -e

if [ -z "$LOADTEST_ADMIN_TOKEN" ] && [ -n "$ADMIN_SEED_TOKEN" ]; then
    export LOADTEST_ADMIN_TOKEN="$ADMIN_SEED_TOKEN"
fi

exec "$@"
