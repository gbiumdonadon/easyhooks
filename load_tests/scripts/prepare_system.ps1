# Prepare system for load testing (PowerShell version)
# Run this before starting load tests to optimize system limits

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "Preparing system for load testing" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""

# 1. Check Docker Desktop resources
Write-Host "[1/4] Checking Docker Desktop configuration..." -ForegroundColor Yellow
Write-Host "  Please ensure Docker Desktop has:" -ForegroundColor White
Write-Host "    - Memory: 8GB+" -ForegroundColor Gray
Write-Host "    - CPUs: 4+" -ForegroundColor Gray
Write-Host "    - Disk: 20GB+" -ForegroundColor Gray
Write-Host "  Settings -> Resources -> Advanced" -ForegroundColor Gray
Write-Host ""

# 2. Check if Docker is running
Write-Host "[2/4] Checking Docker containers..." -ForegroundColor Yellow
try {
    $containers = docker ps --format "{{.Names}}" 2>$null
    
    if ($containers -match "easyhooks-app") {
        Write-Host "  Updating resource limits for running containers..." -ForegroundColor White
        
        # Update app
        docker update --cpus="4" --memory="4g" easyhooks-app-1 2>$null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "  ✓ Updated easyhooks-app-1" -ForegroundColor Green
        }
        
        # Update workers
        $workers = docker ps --format "{{.Names}}" | Select-String "easyhooks-worker"
        foreach ($worker in $workers) {
            docker update --cpus="2" --memory="2g" $worker 2>$null
        }
        Write-Host "  ✓ Updated workers" -ForegroundColor Green
        
        # Update kafka
        docker update --cpus="2" --memory="2g" easyhooks-kafka-1 2>$null
        
        # Update redis
        docker update --cpus="2" --memory="2g" easyhooks-redis-1 2>$null
        
        # Update postgres
        docker update --cpus="2" --memory="2g" easyhooks-db-1 2>$null
        
        Write-Host "  ✓ Docker containers updated" -ForegroundColor Green
    } else {
        Write-Host "  ⚠ Containers not running. Start them first:" -ForegroundColor Yellow
        Write-Host "    docker compose up -d" -ForegroundColor Gray
    }
} catch {
    Write-Host "  ⚠ Docker not found or not running" -ForegroundColor Yellow
}
Write-Host ""

# 3. Kafka topic configuration
Write-Host "[3/4] Configuring Kafka topics..." -ForegroundColor Yellow
try {
    $kafkaRunning = docker ps --format "{{.Names}}" | Select-String "easyhooks-kafka"
    
    if ($kafkaRunning) {
        # Recreate topics with more partitions
        docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh `
            --bootstrap-server localhost:9092 `
            --delete --topic webhooks.inbound 2>$null
        
        docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh `
            --bootstrap-server localhost:9092 `
            --create --topic webhooks.inbound `
            --partitions 8 `
            --replication-factor 1 `
            --config retention.ms=3600000 2>$null
        
        if ($LASTEXITCODE -eq 0) {
            Write-Host "  ✓ Kafka topic configured with 8 partitions" -ForegroundColor Green
        } else {
            Write-Host "  ⚠ Could not configure Kafka" -ForegroundColor Yellow
        }
    } else {
        Write-Host "  ⚠ Kafka container not running" -ForegroundColor Yellow
    }
} catch {
    Write-Host "  ⚠ Could not configure Kafka" -ForegroundColor Yellow
}
Write-Host ""

# 4. System information
Write-Host "[4/4] System information:" -ForegroundColor Yellow
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
Write-Host "     docker compose -f load_tests/docker-compose.loadtest.yml run --rm --no-deps -e TENANT_POOL_FILE=/load_tests/.tenant_pool.json k6 run k6/scenarios/baseline.js" -ForegroundColor Gray
Write-Host ""
