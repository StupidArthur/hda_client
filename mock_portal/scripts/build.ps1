# 构建 Mock 门户：先构建前端，再编译嵌入前端的独立 exe。
#
# 用法（在任意目录）：
#   powershell -ExecutionPolicy Bypass -File mock_portal/scripts/build.ps1
#   powershell -ExecutionPolicy Bypass -File mock_portal/scripts/build.ps1 -Out D:\dist\mock_portal.exe
param(
    [string]$Out = "mock_portal.exe",
    [switch]$SkipFrontendInstall
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$frontend = Join-Path $root "frontend"

Push-Location $frontend
try {
    if (-not $SkipFrontendInstall) {
        npm.cmd ci --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw "npm ci failed: $LASTEXITCODE" }
    }
    npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw "Frontend build failed: $LASTEXITCODE" }
}
finally {
    Pop-Location
}

Push-Location $root
try {
    if (-not [System.IO.Path]::IsPathRooted($Out)) {
        $Out = Join-Path $root $Out
    }
    go build -trimpath -o $Out ./cmd/mock_portal
    if ($LASTEXITCODE -ne 0) { throw "Go build failed: $LASTEXITCODE" }
    Write-Host "已构建 $Out"
}
finally {
    Pop-Location
}
