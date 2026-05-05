#!/bin/bash
# Prepare system for load testing
# Run this before starting load tests to optimize system limits

set -e

echo "=========================================="
echo "Preparing system for load testing"
echo "=========================================="
echo ""

if [ "$EUID" -ne 0 ]; then
    echo "WARNING: Not running as root. Some optimizations will be skipped."
    echo "Run with sudo for full optimization: sudo $0"
    echo ""
fi

# 1. Increase file descriptor limits
echo "[1/4] Increasing file descriptor limits..."
ulimit -n 65536 2>/dev/null || echo "  ⚠ Could not set ulimit (may need sudo)"
echo "  Current ulimit -n: $(ulimit -n)"
echo ""

# 2. TCP tuning (requires root)
if [ "$EUID" -eq 0 ]; then
    echo "[2/4] Tuning TCP parameters..."
    sysctl -w net.core.somaxconn=4096
    sysctl -w net.ipv4.tcp_max_syn_backlog=4096
    sysctl -w net.ipv4.ip_local_port_range="1024 65535"
    sysctl -w net.ipv4.tcp_fin_timeout=30
    sysctl -w net.ipv4.tcp_tw_reuse=1
    echo "  ✓ TCP parameters tuned"
else
    echo "[2/4] Skipping TCP tuning (requires root)"
fi
echo ""

# 3. Docker container resource limits (Redis-only stack)
echo "[3/4] Checking Docker containers..."
if command -v docker &> /dev/null; then
    if docker ps --format '{{.Names}}' | grep -q "easyhooks-app"; then
        echo "  Updating resource limits for running containers..."

        docker update --cpus="4" --memory="4g" easyhooks-app-1 2>/dev/null || \
            echo "  ⚠ Could not update easyhooks-app-1"

        for worker in $(docker ps --format '{{.Names}}' | grep "easyhooks-worker"); do
            docker update --cpus="2" --memory="2g" "$worker" 2>/dev/null || \
                echo "  ⚠ Could not update $worker"
        done

        docker update --cpus="2" --memory="2g" easyhooks-redis-1 2>/dev/null || \
            echo "  ⚠ Could not update easyhooks-redis-1"

        echo "  ✓ Docker containers updated"
    else
        echo "  ⚠ Containers not running. Start them first:"
        echo "    docker compose up -d"
    fi
else
    echo "  ⚠ Docker not found"
fi
echo ""

# 4. System information
echo "[4/4] System information:"
echo "  CPU cores: $(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 'unknown')"
echo "  Memory: $(free -h 2>/dev/null | awk '/^Mem:/ {print $2}' || echo 'unknown')"
echo "  Docker version: $(docker --version 2>/dev/null || echo 'not installed')"
echo ""

echo "=========================================="
echo "System preparation complete!"
echo "=========================================="
echo ""
echo "Next steps:"
echo "  1. Start the stack: docker compose up -d"
echo "  2. Create test tenants: cd load_tests && bash scripts/create_tenant_pool.sh"
echo "  3. Run k6: docker compose -f load_tests/docker-compose.loadtest.yml run --rm k6 run k6/scenarios/baseline.js"
echo ""
echo "The events:in / events:failed Redis Streams and the webhook-workers"
echo "consumer group are created automatically by the API and worker on startup —"
echo "no manual step is needed."
echo ""
