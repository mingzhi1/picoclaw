#!/usr/bin/env pwsh
# live_test_scheduler.ps1 — 定时运行 PicoClaw live integration tests
#
# 用法:
#   # 每 30 分钟循环一次（默认）
#   .\scripts\live_test_scheduler.ps1
#
#   # 自定义间隔
#   .\scripts\live_test_scheduler.ps1 -IntervalMinutes 60
#
#   # 只跑一次（CI / 手动验证）
#   .\scripts\live_test_scheduler.ps1 -RunOnce
#
#   # 只跑工具类测试，一次
#   .\scripts\live_test_scheduler.ps1 -Filter "TestLive_Tools" -RunOnce
#
#   # 跑所有 live 测试，含长对话
#   .\scripts\live_test_scheduler.ps1 -Filter "TestLive_" -TimeoutSeconds 900 -RunOnce
param(
    [int]    $IntervalMinutes = 30,
    [string] $Filter          = "TestLive_Tools|TestLive_Phase",  # 默认不跑耗时的 Long
    [int]    $TimeoutSeconds  = 300,
    [string] $LiveSleep       = "4s",
    [switch] $RunOnce
)

$projectRoot = Split-Path $PSScriptRoot -Parent
$outDir      = Join-Path $projectRoot ".tmp\test_runs"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

$csvFile = Join-Path $outDir "summary.csv"
if (-not (Test-Path $csvFile)) {
    "Run,Timestamp,Status,Pass,Fail,TotalTokens,PromptTokens,CompletionTokens,DurationSec,LogFile" |
        Out-File $csvFile -Encoding utf8
}

function Get-TokenSummary {
    param([string]$LogDir)
    $total = 0; $prompt = 0; $comp = 0
    Get-ChildItem $LogDir -Recurse -Filter "*.json" -ErrorAction SilentlyContinue |
    ForEach-Object {
        try {
            $j = Get-Content $_.FullName -Raw | ConvertFrom-Json
            if ($j.response.usage) {
                $prompt += [int]$j.response.usage.prompt_tokens
                $comp   += [int]$j.response.usage.completion_tokens
                $total  += [int]$j.response.usage.total_tokens
            }
        } catch {}
    }
    return @{ prompt=$prompt; comp=$comp; total=$total }
}

function Run-Tests {
    param([int]$RunNum)

    $timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
    $logFile   = Join-Path $outDir "run_${timestamp}.txt"

    # Clear old prompt logs so we only count new ones
    $promptLogDir = Join-Path $projectRoot "pkg\agent\testdata\prompt_logs"
    Remove-Item "$promptLogDir\*" -Recurse -Force -ErrorAction SilentlyContinue

    Write-Host ""
    Write-Host "=== Run #$RunNum  [$timestamp]  filter='$Filter' ===" -ForegroundColor Yellow
    Write-Host "    timeout=${TimeoutSeconds}s  sleep=$LiveSleep" -ForegroundColor DarkGray

    $env:LIVE_SLEEP = $LiveSleep
    $sw = [Diagnostics.Stopwatch]::StartNew()

    $result = & go test ./pkg/agent/... `
        -run $Filter `
        -v `
        -timeout "${TimeoutSeconds}s" 2>&1

    $sw.Stop()
    $exitCode = $LASTEXITCODE
    $result | Out-File -FilePath $logFile -Encoding utf8

    # Parse results
    $passLines = $result | Select-String "^--- PASS"
    $failLines = $result | Select-String "^--- FAIL"
    $passCount = $passLines.Count
    $failCount = $failLines.Count
    $status    = if ($exitCode -eq 0) { "PASS" } else { "FAIL" }
    $color     = if ($exitCode -eq 0) { "Green" } else { "Red" }
    $durationSec = [int]$sw.Elapsed.TotalSeconds

    # Token summary from prompt log files
    $tokens = Get-TokenSummary -LogDir $promptLogDir

    Write-Host "  Status: $status  pass=$passCount  fail=$failCount  time=${durationSec}s" -ForegroundColor $color
    Write-Host "  Tokens: prompt=$($tokens.prompt)  completion=$($tokens.comp)  total=$($tokens.total)" -ForegroundColor Cyan

    if ($failLines) {
        Write-Host "  Failed tests:" -ForegroundColor Red
        $failLines | ForEach-Object { Write-Host "    $_" -ForegroundColor Red }
    }

    Write-Host "  Log: $logFile" -ForegroundColor DarkGray

    # Append to CSV
    "$RunNum,$timestamp,$status,$passCount,$failCount,$($tokens.total),$($tokens.prompt),$($tokens.comp),$durationSec,$logFile" |
        Add-Content $csvFile

    return $exitCode
}

function Show-Trend {
    if (-not (Test-Path $csvFile)) { return }
    $rows = Import-Csv $csvFile
    if ($rows.Count -lt 2) { return }

    Write-Host ""
    Write-Host "=== Recent Trend (last 5 runs) ===" -ForegroundColor Cyan
    $rows | Select-Object -Last 5 |
        Format-Table Run, Timestamp, Status, Pass, Fail, TotalTokens, DurationSec -AutoSize
}

# --- Main ---
$run = 0
do {
    $run++
    $code = Run-Tests -RunNum $run
    Show-Trend

    if ($RunOnce) { exit $code }

    Write-Host ""
    Write-Host "Next run in $IntervalMinutes minute(s). Ctrl+C to stop." -ForegroundColor DarkGray
    Start-Sleep -Seconds ($IntervalMinutes * 60)
} while ($true)
