# Script de Diagnóstico - Testes de Carga EasyHooks
# Este script verifica se o ambiente está pronto para testes de carga

Write-Host "=" -NoNewline -ForegroundColor Cyan
Write-Host ("=" * 69) -ForegroundColor Cyan
Write-Host "DIAGNOSTICO DO AMBIENTE DE TESTES DE CARGA" -ForegroundColor Cyan
Write-Host ("=" * 70) -ForegroundColor Cyan
Write-Host ""

$allOk = $true

# Função auxiliar para status
function Write-Status {
    param(
        [string]$Message,
        [string]$Status,
        [string]$Details = ""
    )
    
    $statusColor = switch ($Status) {
        "OK" { "Green" }
        "WARN" { "Yellow" }
        "ERROR" { "Red" }
        "INFO" { "Cyan" }
    }
    
    $statusSymbol = switch ($Status) {
        "OK" { "[OK]" }
        "WARN" { "[!]" }
        "ERROR" { "[X]" }
        "INFO" { "[i]" }
    }
    
    Write-Host "$statusSymbol " -NoNewline -ForegroundColor $statusColor
    Write-Host $Message -NoNewline
    if ($Details) {
        Write-Host " - " -NoNewline -ForegroundColor Gray
        Write-Host $Details -ForegroundColor Gray
    } else {
        Write-Host ""
    }
}

# 1. Verificar Docker
Write-Host "DOCKER" -ForegroundColor Yellow
Write-Host ""

try {
    $dockerVersion = docker --version 2>$null
    if ($LASTEXITCODE -eq 0) {
        Write-Status "Docker instalado" "OK" "$dockerVersion"
    } else {
        Write-Status "Docker não encontrado" "ERROR" "Instale o Docker Desktop"
        $allOk = $false
    }
} catch {
    Write-Status "Docker não encontrado" "ERROR" "Instale o Docker Desktop"
    $allOk = $false
}

try {
    $dockerRunning = docker ps 2>$null
    if ($LASTEXITCODE -eq 0) {
        Write-Status "Docker está rodando" "OK"
    } else {
        Write-Status "Docker não está rodando" "ERROR" "Inicie o Docker Desktop"
        $allOk = $false
    }
} catch {
    Write-Status "Docker não está rodando" "ERROR" "Inicie o Docker Desktop"
    $allOk = $false
}

Write-Host ""

# 2. Verificar serviços
Write-Host "SERVICOS" -ForegroundColor Yellow
Write-Host ""

$services = @("db", "redis", "kafka", "app", "worker")

foreach ($service in $services) {
    try {
        $status = docker compose ps $service --format json 2>$null | ConvertFrom-Json
        
        if ($status) {
            $state = $status.State
            $health = $status.Health
            
            if ($state -eq "running") {
                if ($health -eq "healthy" -or $health -eq "") {
                    Write-Status "$service está rodando" "OK" "Estado: $state"
                } else {
                    Write-Status "$service está rodando mas não saudável" "WARN" "Health: $health"
                }
            } else {
                Write-Status "$service não está rodando" "ERROR" "Estado: $state"
                $allOk = $false
            }
        } else {
            Write-Status "$service não encontrado" "ERROR" "Execute docker compose up -d"
            $allOk = $false
        }
    } catch {
        Write-Status "$service status desconhecido" "WARN" "Erro ao verificar"
    }
}

Write-Host ""

# 3. Verificar Locust
Write-Host "LOCUST" -ForegroundColor Yellow
Write-Host ""

try {
    $locustVersion = locust --version 2>$null
    if ($LASTEXITCODE -eq 0) {
        Write-Status "Locust instalado" "OK" "$locustVersion"
    } else {
        Write-Status "Locust não encontrado" "ERROR" "Execute: pip install -r load_tests/requirements.txt"
        $allOk = $false
    }
} catch {
    Write-Status "Locust não encontrado" "ERROR" "Execute: pip install -r load_tests/requirements.txt"
    $allOk = $false
}

# Verificar se Locust está rodando no Docker
try {
    $locustMaster = docker compose ps locust-master --format json 2>$null | ConvertFrom-Json
    if ($locustMaster -and $locustMaster.State -eq "running") {
        Write-Status "Locust Master rodando (Docker)" "OK" "Porta: 8089"
    } else {
        Write-Status "Locust Master não está rodando" "INFO" "Opcional - use locust CLI local"
    }
} catch {
    Write-Status "Locust Master não encontrado" "INFO" "Opcional - use locust CLI local"
}

Write-Host ""

# 4. Verificar pool de tenants
Write-Host "TENANTS" -ForegroundColor Yellow
Write-Host ""

$tenantPoolFile = "load_tests\.tenant_pool.json"
if (Test-Path $tenantPoolFile) {
    try {
        $tenantPool = Get-Content $tenantPoolFile | ConvertFrom-Json
        $tenantCount = $tenantPool.tenants.Count
        
        if ($tenantCount -gt 0) {
            Write-Status "Pool de tenants existe" "OK" "$tenantCount tenants encontrados"
            
            if ($tenantCount -lt 10) {
                Write-Status "Poucos tenants no pool" "WARN" "Recomendado: 50+ para testes de carga"
            }
        } else {
            Write-Status "Pool de tenants está vazio" "ERROR" "Execute: python load_tests/utils/tenant_factory.py --create --count 50"
            $allOk = $false
        }
    } catch {
        Write-Status "Erro ao ler pool de tenants" "ERROR" "Arquivo corrompido ou inválido"
        $allOk = $false
    }
} else {
    Write-Status "Pool de tenants não existe" "ERROR" "Execute: python load_tests/utils/tenant_factory.py --create --count 50"
    $allOk = $false
}

Write-Host ""

# 5. Verificar variaveis de ambiente
Write-Host "CONFIGURACAO" -ForegroundColor Yellow
Write-Host ""

$envFile = ".env"
if (Test-Path $envFile) {
    Write-Status "Arquivo .env existe" "OK"
    
    # Verificar variáveis críticas
    $envContent = Get-Content $envFile -Raw
    
    $criticalVars = @(
        "POSTGRES_PASSWORD",
        "DATABASE_URL",
        "ADMIN_SEED_TOKEN",
        "APP_SECRET_KEY"
    )
    
    foreach ($var in $criticalVars) {
        if ($envContent -match "$var\s*=\s*.+") {
            Write-Status "  $var definido" "OK"
        } else {
            Write-Status "  $var não definido" "WARN" "Pode causar problemas"
        }
    }
} else {
    Write-Status "Arquivo .env não encontrado" "WARN" "Crie baseado no .env.example"
}

Write-Host ""

# 6. Verificar recursos do Docker
Write-Host "RECURSOS" -ForegroundColor Yellow
Write-Host ""

try {
    $dockerInfo = docker info 2>$null
    if ($LASTEXITCODE -eq 0) {
        # Extrair informações de memória e CPUs
        $cpus = ($dockerInfo | Select-String -Pattern "CPUs:\s*(\d+)").Matches.Groups[1].Value
        $memory = ($dockerInfo | Select-String -Pattern "Total Memory:\s*([\d.]+\s*\w+)").Matches.Groups[1].Value
        
        Write-Status "CPUs disponíveis" "INFO" "$cpus cores"
        Write-Status "Memória disponível" "INFO" "$memory"
        
        # Verificar se há recursos suficientes
        if ([int]$cpus -lt 4) {
            Write-Status "Recursos de CPU limitados" "WARN" "Recomendado: 4+ cores para testes de carga"
        }
    }
} catch {
    Write-Status "Não foi possível verificar recursos" "WARN"
}

Write-Host ""

# 7. Verificar conectividade
Write-Host "CONECTIVIDADE" -ForegroundColor Yellow
Write-Host ""

$endpoints = @{
    "API" = "http://localhost:8000/health"
    "Locust UI" = "http://localhost:8089"
    "Grafana" = "http://localhost:3000"
    "Prometheus" = "http://localhost:9090"
}

foreach ($endpoint in $endpoints.GetEnumerator()) {
    try {
        $response = Invoke-WebRequest -Uri $endpoint.Value -Method GET -TimeoutSec 2 -UseBasicParsing 2>$null
        if ($response.StatusCode -eq 200 -or $response.StatusCode -eq 302) {
            Write-Status "$($endpoint.Key) acessível" "OK" "$($endpoint.Value)"
        } else {
            Write-Status "$($endpoint.Key) retornou código $($response.StatusCode)" "WARN" "$($endpoint.Value)"
        }
    } catch {
        Write-Status "$($endpoint.Key) não acessível" "WARN" "$($endpoint.Value) - Serviço pode não estar rodando"
    }
}

Write-Host ""

# 8. Verificar arquivos de teste
Write-Host "ARQUIVOS DE TESTE" -ForegroundColor Yellow
Write-Host ""

$testFiles = @(
    "load_tests/locustfile.py",
    "load_tests/config.py",
    "load_tests/scenarios/baseline.py",
    "load_tests/scenarios/throughput.py",
    "load_tests/utils/tenant_factory.py"
)

foreach ($file in $testFiles) {
    if (Test-Path $file) {
        Write-Status "$(Split-Path $file -Leaf)" "OK"
    } else {
        Write-Status "$(Split-Path $file -Leaf)" "ERROR" "Arquivo não encontrado: $file"
        $allOk = $false
    }
}

Write-Host ""

# Resumo final
Write-Host ("=" * 70) -ForegroundColor Cyan
if ($allOk) {
    Write-Host "DIAGNOSTICO COMPLETO - AMBIENTE OK" -ForegroundColor Green
    Write-Host ""
    Write-Host "Proximos passos:" -ForegroundColor Yellow
    Write-Host "  1. Executar teste baseline:" -ForegroundColor Gray
    Write-Host "     locust -f load_tests/scenarios/baseline.py --host=http://localhost:8000" -ForegroundColor Gray
    Write-Host ""
    Write-Host "  2. Acessar interface web:" -ForegroundColor Gray
    Write-Host "     http://localhost:8089" -ForegroundColor Gray
} else {
    Write-Host "DIAGNOSTICO COMPLETO - PROBLEMAS ENCONTRADOS" -ForegroundColor Red
    Write-Host ""
    Write-Host "Corrija os erros acima antes de executar testes." -ForegroundColor Yellow
}
Write-Host ("=" * 70) -ForegroundColor Cyan
Write-Host ""

# Informacoes adicionais uteis
Write-Host "INFORMACOES UTEIS" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Documentacao:" -ForegroundColor Gray
Write-Host "    - Guia de parametros: load_tests/PARAMETROS_TESTE.md" -ForegroundColor Gray
Write-Host "    - README completo: load_tests/README.md" -ForegroundColor Gray
Write-Host ""
Write-Host "  Comandos uteis:" -ForegroundColor Gray
Write-Host "    - Ver logs: docker compose logs -f app" -ForegroundColor Gray
Write-Host "    - Recriar servicos: docker compose down -v; docker compose up -d" -ForegroundColor Gray
Write-Host "    - Criar tenants: python load_tests/utils/tenant_factory.py --create --count 50" -ForegroundColor Gray
Write-Host ""
