<#
.SYNOPSIS
    Runs the capacity benchmark across the small/medium/large profiles.

.DESCRIPTION
    For each profile, recreates app+worker with the matching mem_limit and
    EASYHOOKS_PROFILE, drains the events:in stream so each tier starts from
    zero, drives k6 with 100 VUs for $DurationSec seconds, and parses the
    resulting JSON summary.

    Output: a CSV at load_tests/reports/capacity/capacity-summary.csv plus a
    per-profile JSON copy. The script also prints a markdown-friendly table
    for the README.

.PARAMETER DurationSec
    How long to drive each profile (default 30s).

.PARAMETER Vus
    How many concurrent virtual users k6 should run (default 100). The bigger
    the number, the more aggressive the load.
#>
[CmdletBinding()]
param(
    [int]$DurationSec = 30,
    [int]$Vus = 100
)

$ErrorActionPreference = 'Continue'
# Docker writes status lines to stderr. PowerShell treats them as errors when
# $ErrorActionPreference=Stop, which aborts the script mid-run.
$PSNativeCommandUseErrorActionPreference = $false
$repoRoot = (Resolve-Path "$PSScriptRoot/../..").Path
Set-Location $repoRoot

$profiles = @(
    @{ Name = 'small';  MemLimit = '256m' },
    @{ Name = 'medium'; MemLimit = '512m' },
    @{ Name = 'large';  MemLimit = '1g'  }
)

$reportsDir = Join-Path $repoRoot 'load_tests\reports\capacity'
New-Item -ItemType Directory -Force -Path $reportsDir | Out-Null

$results = @()

foreach ($p in $profiles) {
    Write-Host "`n==================== Profile: $($p.Name) ($($p.MemLimit)) ====================" -ForegroundColor Cyan

    $env:EASYHOOKS_PROFILE = $p.Name
    $env:EASYHOOKS_MEM_LIMIT = $p.MemLimit

    Write-Host "Recreating app+worker..." -ForegroundColor Yellow
    docker compose -f docker-compose.yml -f docker-compose.capacity.yml up -d --force-recreate app worker | Out-Null

    Write-Host "Waiting for /health..." -ForegroundColor Yellow
    $healthy = $false
    for ($i = 0; $i -lt 30; $i++) {
        try {
            $resp = Invoke-WebRequest -Uri 'http://localhost:8000/health' -UseBasicParsing -TimeoutSec 2 -ErrorAction Stop
            if ($resp.StatusCode -eq 200) { $healthy = $true; break }
        } catch { Start-Sleep -Milliseconds 500 }
    }
    if (-not $healthy) {
        Write-Warning "API did not become healthy within 15s for profile $($p.Name); skipping."
        continue
    }

    Write-Host "Draining events:in so the queue starts empty..." -ForegroundColor Yellow
    docker compose exec -T redis redis-cli DEL events:in events:failed | Out-Null
    docker compose exec -T redis redis-cli XGROUP CREATE events:in webhook-workers '$' MKSTREAM 2>$null | Out-Null
    Start-Sleep -Seconds 2

    Write-Host "Running k6 (vus=$Vus, duration=${DurationSec}s)..." -ForegroundColor Yellow
    docker compose -f load_tests/docker-compose.loadtest.yml run --rm --no-deps k6 `
        run --vus $Vus --duration "${DurationSec}s" --no-thresholds `
        k6/scenarios/throughput.js 2>&1 | Out-File (Join-Path $reportsDir "$($p.Name)-k6.log")

    $summaryPath = Join-Path $repoRoot 'load_tests\reports\throughput-summary.json'
    if (-not (Test-Path $summaryPath)) {
        Write-Warning "No summary produced for profile $($p.Name)"
        continue
    }
    $summary = Get-Content $summaryPath -Raw | ConvertFrom-Json
    $rps     = [math]::Round($summary.metrics.http_reqs.values.rate, 0)
    $reqs    = $summary.metrics.http_reqs.values.count
    $p95     = [math]::Round($summary.metrics.http_req_duration.values.'p(95)', 2)
    $avg     = [math]::Round($summary.metrics.http_req_duration.values.avg, 2)
    $maxLat  = [math]::Round($summary.metrics.http_req_duration.values.max, 2)
    $failPct = [math]::Round($summary.metrics.http_req_failed.values.rate * 100, 2)

    # Checks pass = 202 Accepted; fails = 429 (or other errors)
    $accepted = $summary.metrics.checks.values.passes
    $rejected = $summary.metrics.checks.values.fails

    Copy-Item $summaryPath -Destination (Join-Path $reportsDir "$($p.Name)-throughput.json") -Force

    $stats = docker stats --no-stream --format "{{.MemUsage}}" easyhooks-app-1
    $workerStats = docker stats --no-stream --format "{{.MemUsage}}" easyhooks-worker-1
    Write-Host "Profile $($p.Name): rps=$rps reqs=$reqs accepted=$accepted rejected=$rejected p95=${p95}ms avg=${avg}ms fail=${failPct}% app_mem=$stats worker_mem=$workerStats" -ForegroundColor Green

    $results += [PSCustomObject]@{
        Profile = $p.Name
        MemLimit = $p.MemLimit
        Rps = $rps
        Accepted = $accepted
        Rejected429 = $rejected
        AvgMs = $avg
        P95Ms = $p95
        MaxMs = $maxLat
        FailPct = $failPct
        AppMem = $stats
        WorkerMem = $workerStats
    }
}

Write-Host "`n==================== Final summary ====================" -ForegroundColor Cyan
$results | Format-Table -AutoSize
$results | Export-Csv -Path (Join-Path $reportsDir 'capacity-summary.csv') -NoTypeInformation
Write-Host "Saved CSV to $reportsDir\capacity-summary.csv"

Write-Host "`nMarkdown table for the README:" -ForegroundColor Cyan
Write-Host "| Profile | Container | Sustained RPS | 202 Accepted | 429 Shed | p95 latency | App RSS |"
Write-Host "|---------|-----------|---------------|--------------|----------|-------------|---------|"
foreach ($r in $results) {
    $appMem = ($r.AppMem -split '/')[0].Trim()
    Write-Host ("| {0} | {1} | {2} | {3} | {4} | {5} ms | {6} |" -f $r.Profile, $r.MemLimit, $r.Rps, $r.Accepted, $r.Rejected429, $r.P95Ms, $appMem)
}
