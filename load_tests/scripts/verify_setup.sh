#!/bin/bash
# Verify load test setup

set -e

echo "=========================================="
echo "Load Test Setup Verification"
echo "=========================================="
echo ""

ERRORS=0

# 1. Check Python
echo "[1/8] Checking Python..."
if command -v python &> /dev/null || command -v python3 &> /dev/null; then
    PYTHON_CMD=$(command -v python3 || command -v python)
    PYTHON_VERSION=$($PYTHON_CMD --version)
    echo "  ✓ $PYTHON_VERSION"
else
    echo "  ✗ Python not found"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 2. Check Docker
echo "[2/8] Checking Docker..."
if command -v docker &> /dev/null; then
    DOCKER_VERSION=$(docker --version)
    echo "  ✓ $DOCKER_VERSION"
    
    # Check if Docker is running
    if docker ps &> /dev/null; then
        echo "  ✓ Docker daemon is running"
    else
        echo "  ✗ Docker daemon is not running"
        ERRORS=$((ERRORS + 1))
    fi
else
    echo "  ✗ Docker not found"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 3. Check containers
echo "[3/8] Checking EasyHooks containers..."
if docker ps --format '{{.Names}}' | grep -q "easyhooks-app"; then
    RUNNING=$(docker ps --filter "name=easyhooks" --format '{{.Names}}' | wc -l)
    echo "  ✓ Found $RUNNING running containers"
    
    # Check health
    UNHEALTHY=$(docker ps --filter "name=easyhooks" --filter "health=unhealthy" --format '{{.Names}}')
    if [ -n "$UNHEALTHY" ]; then
        echo "  ⚠ Unhealthy containers: $UNHEALTHY"
    fi
else
    echo "  ✗ EasyHooks containers not running"
    echo "    Run: docker compose up -d"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 4. Check dependencies
echo "[4/8] Checking Python dependencies..."
if [ -f "requirements.txt" ]; then
    if $PYTHON_CMD -c "import locust" 2>/dev/null; then
        echo "  ✓ locust installed"
    else
        echo "  ✗ locust not installed"
        echo "    Run: pip install -r requirements.txt"
        ERRORS=$((ERRORS + 1))
    fi
else
    echo "  ✗ requirements.txt not found"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 5. Check tenant pool
echo "[5/8] Checking tenant pool..."
if [ -f ".tenant_pool.json" ]; then
    TENANT_COUNT=$(jq '. | length' .tenant_pool.json 2>/dev/null || echo "0")
    echo "  ✓ Tenant pool exists ($TENANT_COUNT tenants)"
    
    if [ "$TENANT_COUNT" -lt 10 ]; then
        echo "  ⚠ Only $TENANT_COUNT tenants (recommend 50+)"
    fi
else
    echo "  ✗ Tenant pool not found"
    echo "    Run: python utils/tenant_factory.py --create --count 50"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 6. Check API accessibility
echo "[6/8] Checking API accessibility..."
if curl -s -o /dev/null -w "%{http_code}" http://localhost:8000/docs | grep -q "200"; then
    echo "  ✓ API is accessible at http://localhost:8000"
else
    echo "  ✗ API is not accessible"
    echo "    Check if containers are running: docker compose ps"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 7. Check Grafana
echo "[7/8] Checking Grafana..."
if curl -s -o /dev/null -w "%{http_code}" http://localhost:3000 | grep -q "200\|302"; then
    echo "  ✓ Grafana is accessible at http://localhost:3000"
else
    echo "  ⚠ Grafana is not accessible (optional for testing)"
fi
echo ""

# 8. Check system resources
echo "[8/8] Checking system resources..."
if command -v free &> /dev/null; then
    TOTAL_MEM=$(free -g | awk '/^Mem:/ {print $2}')
    echo "  ✓ Total memory: ${TOTAL_MEM}GB"
    
    if [ "$TOTAL_MEM" -lt 8 ]; then
        echo "  ⚠ Recommended: 8GB+ for load testing"
    fi
fi

if command -v nproc &> /dev/null; then
    CPU_CORES=$(nproc)
    echo "  ✓ CPU cores: $CPU_CORES"
    
    if [ "$CPU_CORES" -lt 4 ]; then
        echo "  ⚠ Recommended: 4+ cores for load testing"
    fi
fi
echo ""

# Summary
echo "=========================================="
if [ $ERRORS -eq 0 ]; then
    echo "✓ All checks passed!"
    echo "=========================================="
    echo ""
    echo "Ready to run load tests:"
    echo "  python quick_start.py baseline"
    echo "  python quick_start.py throughput"
    echo ""
    exit 0
else
    echo "✗ $ERRORS error(s) found"
    echo "=========================================="
    echo ""
    echo "Please fix the errors above before running tests."
    echo ""
    exit 1
fi
