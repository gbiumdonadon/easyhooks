# Prepare system for load testing (PowerShell version)
# Run this before starting load tests to optimize system limits.

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "Preparing system for load testing" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""

# 1. Docker Desktop reminders
Write-Host "[1/3] Checking Docker Desktop configuration..." -ForegroundColor Yellow
Write-Host "  Please ensure Docker Desktop has:" -ForegroundColor White
Write-Host "    - Memory: 8GB+" -ForegroundColor Gray
Write-Host "    - CPUs: 4+" -ForegroundColor Gray
Write-Host "    - Disk: 20GB+" -ForegroundColor Gray
Write-Host "  Settings -> Resources -> Advanced" -ForegroundColor Gray
Write-Host ""

# 2. Resource limits on running containers (Redis-only stack)
Write-Host "[2/3] Checking Docker containers..." -ForegroundColor Yellow
try {
    $containers = docker ps --format "{{.Names}}" 2>$null

    if ($containers -match "easyhooks-app") {
        Write-Host "  Updating resource limits for running containers..." -ForegroundColor White

        docker update --cpus="4" --memory="4g" easyhooks-app-1 2>$null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "  ✓ Updated easyhooks-app-1" -ForegroundColor Green
        }

        $workers = docker ps --format "{{.Names}}" | Select-String "easyhooks-worker"
        foreach ($worker in $workers) {
            docker update --cpus="2" --memory="2g" $worker 2>$null
        }
        Write-Host "  ✓ Updated workers" -ForegroundColor Green

        docker update --cpus="2" --memory="2g" easyhooks-redis-1 2>$null
        Write-Host "  ✓ Updated easyhooks-redis-1" -ForegroundColor Green
    } else {
        Write-Host "  ⚠ Containers not running. Start them first:" -ForegroundColor Yellow
        Write-Host "    docker compose up -d" -ForegroundColor Gray
    }
} catch {
    Write-Host "  ⚠ Docker not found or not running" -ForegroundColor Yellow
}
Write-Host ""

# 3. System information
Write-Host "[3/3] System information:" -ForegroundColor Yellow
$cpuCores = (Get-WmiObject Win32_ComputerSystem).NumberOfLogicalProcessors
$memory = [math]::Round((Get-WmiObject Win32_ComputerSystem).TotalPhysicalMemory / 1GB, 2)
Write-Host "  CPU cores: $cpuCores" -ForegroundColor White
Write-Host "  Memory: $memory GB" -ForegroundColor White
try {
    $dockerVersion = docker --version
    Write-Host "  Docker: $dockerVersion" -ForegroundColor White
} catch {
    Write-Host "  Docker: not installed" -ForegroundColor Yellow
}
Write-Host ""

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "System preparation complete!" -ForegroundColor Green
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Next steps:" -ForegroundColor White
Write-Host "  1. Start the stack:" -ForegroundColor Gray
Write-Host "     docker compose up -d" -ForegroundColor Gray
Write-Host "  2. Create test tenants:" -ForegroundColor Gray
Write-Host "     cd load_tests; bash scripts/create_tenant_pool.sh" -ForegroundColor Gray
Write-Host "  3. Run k6 (Docker):" -ForegroundColor Gray
Write-Host "     docker compose -f load_tests/docker-compose.loadtest.yml run --rm k6 run k6/scenarios/baseline.js" -ForegroundColor Gray
Write-Host ""
Write-Host "Note: events:in / events:failed Redis Streams and the webhook-workers" -ForegroundColor Gray
Write-Host "consumer group are created automatically on API/worker startup." -ForegroundColor Gray
Write-Host ""
