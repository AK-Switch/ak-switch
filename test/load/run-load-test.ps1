#requires -Version 5.1

<#
.SYNOPSIS
    运行 AK Switch 代理的负载压测脚本（基于 Vegeta）

.DESCRIPTION
    使用 vegeta 对 AK Switch 代理进行多种场景的负载压测，结果输出到 results/ 目录。
    需要先安装 vegeta: https://github.com/tsenart/vegeta

.PARAMETER Scenario
    压测场景名称。可选值：
    - normal-concurrent  (正常并发，500 QPS, 60s)
    - all-keys-cooldown  (全 Key 冷却)
    - upstream-flaky     (上游 429 抖动)

.PARAMETER Target
    目标代理地址。默认从环境变量 AKSWITCH_TARGET 读取，未设置则默认为 http://localhost:8080

.PARAMETER OutputDir
    结果输出目录。默认 test/load/results/

.PARAMETER ListScenarios
    列出所有可用场景后退出。

.EXAMPLE
    .\run-load-test.ps1 -Scenario normal-concurrent
    .\run-load-test.ps1 -Scenario normal-concurrent -Target http://localhost:4000
    .\run-load-test.ps1 -Scenario all-keys-cooldown
    .\run-load-test.ps1 -ListScenarios
#>

param(
    [Parameter(ParameterSetName = 'Run')]
    [ValidateSet('normal-concurrent', 'all-keys-cooldown', 'upstream-flaky')]
    [string]$Scenario,

    [Parameter(ParameterSetName = 'Run')]
    [string]$Target = $(if ($env:AKSWITCH_TARGET) { $env:AKSWITCH_TARGET } else { 'http://localhost:8080' }),

    [Parameter(ParameterSetName = 'Run')]
    [string]$OutputDir = $(Join-Path $PSScriptRoot 'results'),

    [Parameter(ParameterSetName = 'ListScenarios')]
    [switch]$ListScenarios
)

function Show-Scenarios {
    Write-Host '可用场景:' -ForegroundColor Cyan
    Write-Host '  normal-concurrent  - 正常并发 (500 QPS, 60s)'
    Write-Host '  all-keys-cooldown  - 全 Key 冷却 (模拟所有 Key 被限流)'
    Write-Host '  upstream-flaky     - 上游 429 抖动 (模拟上游不稳定)'
}

function Assert-VegetaInstalled {
    $vegetaPath = (Get-Command 'vegeta' -ErrorAction SilentlyContinue).Source
    if (-not $vegetaPath) {
        Write-Error @"
vegeta 未安装。

请先安装 vegeta:
  1. 访问 https://github.com/tsenart/vegeta/releases 下载对应平台版本
  2. 将 vegeta.exe 放到 PATH 中

或使用 Chocolatey / winget 安装:
  winget install vegeta
  choco install vegeta
"@
        exit 1
    }
    Write-Host "vegeta 路径: $vegetaPath" -ForegroundColor Gray
}

function Get-ScenarioConfig {
    param([string]$Name)
    $configPath = Join-Path $PSScriptRoot 'scenarios' "$Name.json"
    if (-not (Test-Path $configPath)) {
        Write-Error "场景配置不存在: $configPath"
        exit 1
    }
    $config = Get-Content $configPath -Raw | ConvertFrom-Json
    return $config, $configPath
}

function Invoke-LoadTest {
    param(
        [string]$Name,
        [string]$TargetUrl,
        [string]$OutputDirectory,
        [PSCustomObject]$Config
    )

    $timestamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $reportDir = Join-Path $OutputDirectory "$Name-$timestamp"
    New-Item -ItemType Directory -Path $reportDir -Force | Out-Null

    $targetFile  = Join-Path $reportDir 'targets.txt'
    $resultsFile = Join-Path $reportDir 'results.bin'
    $reportFile  = Join-Path $reportDir 'report.txt'
    $plotFile    = Join-Path $reportDir 'plot.html'
    $jsonFile    = Join-Path $reportDir 'report.json'

    # 生成 vegeta target 文件（仅方法+URL，body/header 用 flags 传入）
    "$($Config.target.method) $TargetUrl$($Config.target.path)" | Out-File -Encoding utf8 -FilePath $targetFile
    Write-Host "目标 URL: $TargetUrl$($Config.target.path)" -ForegroundColor Cyan

    # 构建 vegeta attack 命令
    # 检查 body 文件是否存在
    $bodyFile = $null
    $extraHeaders = @()
    if ($Config.target.method -ne 'GET' -and $Config.target.body) {
        $bodyFile = Join-Path $reportDir 'body.json'
        $Config.target.body | Out-File -Encoding utf8 -FilePath $bodyFile
    }
    foreach ($h in $Config.target.headers.PSObject.Properties) {
        $extraHeaders += "-header=$($h.Name): $($h.Value)"
    }

    # 运行 vegeta attack
    $attackArgs = @(
        'attack'
        "-targets=$targetFile"
        "-rate=$($Config.attack.rate)"
        "-duration=$($Config.attack.duration)s"
        "-workers=$($Config.attack.workers)"
        "-output=$resultsFile"
        "-timeout=$($Config.attack.timeout)s"
        "-keepalive=$($Config.attack.keepalive)"
    )
    if ($bodyFile) { $attackArgs += "-body=$bodyFile" }
    $attackArgs += $extraHeaders

    Write-Host "执行: vegeta $($attackArgs -join ' ')" -ForegroundColor Gray
    $result = & vegeta $attackArgs 2>&1
    $attackLog = "$reportDir\attack.err"
    $result | Out-File -Encoding utf8 -FilePath $attackLog

    # 生成文本报告
    Write-Host "`n======= 报告摘要 =======" -ForegroundColor Yellow
    & vegeta report -output=$reportFile $resultsFile
    Get-Content $reportFile | ForEach-Object { Write-Host $_ }

    # 生成 JSON 报告
    & vegeta report -type=json -output=$jsonFile $resultsFile

    # 生成 HTML 图表
    & vegeta report -type=plot -output=$plotFile $resultsFile
    Write-Host "HTML 图表: $plotFile" -ForegroundColor Green
    Write-Host "文本报告: $reportFile" -ForegroundColor Green
    Write-Host "JSON 报告: $jsonFile" -ForegroundColor Green

    # 复制场景配置到报告目录
    Copy-Item (Join-Path $PSScriptRoot 'scenarios' "$Name.json") -Destination (Join-Path $reportDir 'scenario.json')

    Write-Host "`n======= 压测结束: $Name =======" -ForegroundColor Yellow
}

# ── Entry Point ──────────────────────────────────

if ($ListScenarios) {
    Show-Scenarios
    exit 0
}

if (-not $Scenario) {
    Write-Error "请指定场景名称。使用 -ListScenarios 查看可用场景。"
    exit 1
}

Assert-VegetaInstalled

$config, $configPath = Get-ScenarioConfig -Name $Scenario
Write-Host "加载场景配置: $configPath" -ForegroundColor Gray

Invoke-LoadTest -Name $Scenario -TargetUrl $Target -OutputDirectory $OutputDir -Config $config
