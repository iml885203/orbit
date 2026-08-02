$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$testRoot = Join-Path ([System.IO.Path]::GetTempPath()) "orbit-install-test-$PID"
$fixtures = Join-Path $testRoot "fixtures"
$installDirectory = Join-Path $testRoot "install"
$null = New-Item -ItemType Directory -Force -Path $fixtures, $installDirectory
$server = $null

$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
$asset = switch ($architecture) {
    "x64" { "orbit-windows-amd64.exe" }
    "arm64" { "orbit-windows-arm64.exe" }
    default { throw "unsupported test architecture: $architecture" }
}

function Write-TestRelease {
    param([Parameter(Mandatory)] [string] $Version)

    Push-Location $repoRoot
    try {
        & go build -ldflags "-s -w -X main.version=v$Version -X main.buildTime=2026-07-27T04:44:56Z" -o (Join-Path $fixtures $asset) ./cmd/orbit
        if ($LASTEXITCODE -ne 0) { throw "failed to build fixture $Version" }
    }
    finally {
        Pop-Location
    }
    $checksum = (Get-FileHash -Algorithm SHA256 (Join-Path $fixtures $asset)).Hash.ToLowerInvariant()
    Set-Content -Encoding ascii -Path (Join-Path $fixtures "checksums.txt") -Value "$checksum  $asset"
}

function Start-FixtureServer {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $listener.Start()
    $port = ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
    $listener.Stop()

    $script:server = Start-Process python -ArgumentList @(
        "-m", "http.server", "$port", "--bind", "127.0.0.1", "--directory", $fixtures
    ) -WindowStyle Hidden -PassThru
    $baseUrl = "http://127.0.0.1:$port"
    for ($attempt = 0; $attempt -lt 50; $attempt++) {
        try {
            $null = Invoke-WebRequest -UseBasicParsing "$baseUrl/checksums.txt"
            return $baseUrl
        }
        catch {
            Start-Sleep -Milliseconds 100
        }
    }
    throw "fixture server did not start"
}

function Stop-FixtureServer {
    if ($script:server -and -not $script:server.HasExited) {
        Stop-Process -Id $script:server.Id -Force
        $script:server.WaitForExit()
    }
    $script:server = $null
}

function Invoke-Installer {
    param([Parameter(Mandatory)] [string] $Version)

    $env:ORBIT_VERSION = "v$Version"
    & (Join-Path $repoRoot "scripts/install.ps1")
}

function Assert-Version {
    param(
        [Parameter(Mandatory)] [string] $Path,
        [Parameter(Mandatory)] [string] $Version
    )

    $executable = $Path
    $temporaryExecutable = $null
    if ([System.IO.Path]::GetExtension($Path) -ne ".exe") {
        $temporaryExecutable = "$Path.test.exe"
        Copy-Item -Force $Path $temporaryExecutable
        $executable = $temporaryExecutable
    }
    try {
        $actual = & $executable --version
        if ($LASTEXITCODE -ne 0 -or $actual -notmatch "^v$([regex]::Escape($Version)) \([^)]+\)$") {
            throw "$Path reports '$actual', expected v$Version with build time"
        }
    }
    finally {
        if ($temporaryExecutable) {
            Remove-Item -Force -ErrorAction SilentlyContinue $temporaryExecutable
        }
    }
}

$savedEnvironment = @{
    Path = $env:Path
    ORBIT_INSTALL_DIR = $env:ORBIT_INSTALL_DIR
    ORBIT_BASE_URL = $env:ORBIT_BASE_URL
    ORBIT_VERSION = $env:ORBIT_VERSION
    ORBIT_ALLOW_DOWNGRADE = $env:ORBIT_ALLOW_DOWNGRADE
    ORBIT_SKIP_PATH_UPDATE = $env:ORBIT_SKIP_PATH_UPDATE
}
$savedUserPath = [Environment]::GetEnvironmentVariable("Path", "User")

try {
    $env:ORBIT_INSTALL_DIR = $installDirectory
    $target = Join-Path $installDirectory "orbit.exe"

    Write-TestRelease "0.0.1"
    $env:ORBIT_BASE_URL = Start-FixtureServer
    $installOutput = @(Invoke-Installer "0.0.1" 6>&1 | ForEach-Object { "$_" })
    Assert-Version $target "0.0.1"
    if ((Get-Command orbit -ErrorAction Stop).Source -ne $target) {
        throw "newly installed Orbit is not available in the current PowerShell process"
    }
    if (-not ($installOutput -contains "Next: orbit init")) {
        throw "installer did not provide the immediate init command: $($installOutput -join '; ')"
    }

    $sameVersionOutput = @(Invoke-Installer "0.0.1" 6>&1 | ForEach-Object { "$_" })
    if (-not ($sameVersionOutput -contains "Already installed: Orbit 0.0.1 at $target")) {
        throw "same-version install was not reported as a no-op: $($sameVersionOutput -join '; ')"
    }
    if (Test-Path "$target.prev") {
        throw "same-version install replaced the rollback backup"
    }

    $env:ORBIT_SKIP_PATH_UPDATE = "1"

    Write-TestRelease "0.0.0"
    $downgradeBlocked = $false
    try { Invoke-Installer "0.0.0" *> $null } catch { $downgradeBlocked = $true }
    if (-not $downgradeBlocked) { throw "installer unexpectedly downgraded an existing binary" }
    Assert-Version $target "0.0.1"

    $env:ORBIT_ALLOW_DOWNGRADE = "1"
    Invoke-Installer "0.0.0" *> $null
    $env:ORBIT_ALLOW_DOWNGRADE = $null
    Assert-Version $target "0.0.0"
    Assert-Version "$target.prev" "0.0.1"

    Write-TestRelease "0.0.2"
    Set-Content -Encoding ascii -Path (Join-Path $fixtures "checksums.txt") -Value ("0" * 64 + "  $asset")
    $checksumBlocked = $false
    try { Invoke-Installer "0.0.2" *> $null } catch { $checksumBlocked = $true }
    if (-not $checksumBlocked) { throw "installer accepted a bad checksum" }
    Assert-Version $target "0.0.0"
    Assert-Version "$target.prev" "0.0.1"

    Stop-FixtureServer
    $downloadBlocked = $false
    try { Invoke-Installer "0.0.2" *> $null } catch { $downloadBlocked = $true }
    if (-not $downloadBlocked) { throw "installer accepted an interrupted download" }
    Assert-Version $target "0.0.0"
    Assert-Version "$target.prev" "0.0.1"

    Write-TestRelease "0.0.2"
    $env:ORBIT_BASE_URL = Start-FixtureServer
    Invoke-Installer "0.0.2" *> $null
    Assert-Version $target "0.0.2"
    Assert-Version "$target.prev" "0.0.0"

    $leftovers = @(Get-ChildItem $installDirectory -Force |
        Where-Object { $_.Name -like ".orbit-download.*" -or $_.Name -like ".orbit-checksums.*" })
    if ($leftovers.Count -ne 0) {
        throw "installer left temporary files: $($leftovers.Name -join ', ')"
    }
    Write-Host "PowerShell installer no-op, downgrade, interruption, checksum, atomic backup, and cleanup OK"
}
finally {
    Stop-FixtureServer
    [Environment]::SetEnvironmentVariable("Path", $savedUserPath, "User")
    foreach ($name in $savedEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], "Process")
    }
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $testRoot
}
