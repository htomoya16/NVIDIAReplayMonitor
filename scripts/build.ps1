param([string]$Version = 'dev')
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ($Version -notmatch '^[A-Za-z0-9][A-Za-z0-9._+-]*$') { throw 'Invalid version' }
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
try {
    $env:GOCACHE = Join-Path $projectRoot '.cache\go-build'
    $env:GOMODCACHE = Join-Path $projectRoot '.cache\gomod'
    $env:GOTMPDIR = Join-Path $projectRoot '.cache\go-tmp'
    New-Item -ItemType Directory -Force -Path $env:GOTMPDIR | Out-Null
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    go mod download
    if ($LASTEXITCODE -ne 0) { throw 'go mod download failed' }

    $notices = [System.Collections.Generic.List[string]]::new()
    $notices.Add('NVIDIAReplayMonitor - bundled third-party licenses and notices')
    $goRoot = go env GOROOT
    $notices.Add("`n===== Go standard library =====`n" + [IO.File]::ReadAllText((Join-Path $goRoot 'LICENSE')))
    $modules = go list -deps -f '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}' ./cmd/nvidia-replay-monitor | Sort-Object -Unique
    if ($LASTEXITCODE -ne 0) { throw 'go list failed' }
    foreach ($module in $modules) {
        if ([string]::IsNullOrWhiteSpace($module)) { continue }
        $parts = $module.Split('|')
        if ($parts[0] -eq 'nvidiareplaymonitor') { continue }
        $foundLicense = $false
        foreach ($name in @('LICENSE', 'LICENSE.txt', 'LICENSE.md', 'NOTICE', 'NOTICE.txt', 'COPYRIGHT', 'AUTHORS')) {
            $licensePath = Join-Path $parts[2] $name
            if (Test-Path -LiteralPath $licensePath -PathType Leaf) {
                $notices.Add("`n===== $($parts[0]) $($parts[1]) / $name =====`n" + [IO.File]::ReadAllText($licensePath))
                if ($name -like 'LICENSE*') { $foundLicense = $true }
            }
        }
        if (-not $foundLicense) { throw "License not found: $($parts[0])" }
    }
    [IO.File]::WriteAllText((Join-Path $projectRoot 'internal\notices\third_party.txt'), ($notices -join "`n"), [Text.UTF8Encoding]::new($false))

    # Retain work files inside the ignored cache: this also avoids Windows
    # cleanup failures when another process briefly holds a finished exe.
    go test -work -timeout 45s ./...
    if ($LASTEXITCODE -ne 0) { throw 'go test failed' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
    New-Item -ItemType Directory -Force -Path (Join-Path $projectRoot 'dist') | Out-Null
    go build -trimpath -ldflags "-H=windowsgui -s -w -X main.version=$Version" -o dist/NVIDIAReplayMonitor.exe ./cmd/nvidia-replay-monitor
    if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
    $hash = (Get-FileHash -LiteralPath 'dist/NVIDIAReplayMonitor.exe' -Algorithm SHA256).Hash.ToLowerInvariant()
    [IO.File]::WriteAllText((Join-Path $projectRoot 'dist\SHA256SUMS.txt'), "$hash  NVIDIAReplayMonitor.exe`n", [Text.UTF8Encoding]::new($false))
    Write-Output "Built dist/NVIDIAReplayMonitor.exe ($Version)"
} finally {
    Pop-Location
}
