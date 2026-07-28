# Install or update the Orbit CLI on Windows.
#
# Public repository:
#   irm https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.ps1 | iex
#
# Environment overrides:
#   ORBIT_INSTALL_DIR       target directory (default: %LOCALAPPDATA%\Programs\Orbit)
#   ORBIT_VERSION           release tag (default: latest)
#   ORBIT_REPO              GitHub owner/repo (default: iml885203/orbit)
#   ORBIT_BASE_URL          release asset base URL
#   ORBIT_ALLOW_DOWNGRADE=1 permit replacing a newer installed version
#   ORBIT_SKIP_PATH_UPDATE=1 do not add the install directory to the user PATH

$ErrorActionPreference = "Stop"

$Repo = if ($env:ORBIT_REPO) { $env:ORBIT_REPO } else { "iml885203/orbit" }
$RequestedVersion = if ($env:ORBIT_VERSION) { $env:ORBIT_VERSION } else { "latest" }

function Get-OrbitAsset {
    $architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
    switch ($architecture) {
        "x64" { return "orbit-windows-amd64.exe" }
        "arm64" { return "orbit-windows-arm64.exe" }
        default { throw "unsupported Windows architecture: $architecture" }
    }
}

function Get-ReleaseBaseUrl {
    if ($env:ORBIT_BASE_URL) {
        return $env:ORBIT_BASE_URL.TrimEnd("/")
    }
    if ($RequestedVersion -eq "latest") {
        return "https://github.com/$Repo/releases/latest/download"
    }
    return "https://github.com/$Repo/releases/download/$RequestedVersion"
}

function Receive-OrbitAsset {
    param(
        [Parameter(Mandatory)] [string] $Asset,
        [Parameter(Mandatory)] [string] $Destination,
        [Parameter(Mandatory)] [string] $Url
    )

    try {
        Invoke-WebRequest -Uri $Url -OutFile $Destination -UseBasicParsing
        return
    }
    catch {
        if ($env:ORBIT_BASE_URL) {
            throw "download failed: $Url"
        }
        if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
            throw "download failed: $Url`nFor a private repository, install and authenticate GitHub CLI (gh)."
        }
        & gh auth status *> $null
        if ($LASTEXITCODE -ne 0) {
            throw "download failed: $Url`nFor a private repository, authenticate GitHub CLI with 'gh auth login'."
        }
    }

    Write-Host "Anonymous download unavailable; retrying with authenticated GitHub CLI"
    $arguments = @("release", "download")
    if ($RequestedVersion -ne "latest") {
        $arguments += $RequestedVersion
    }
    $arguments += @("--repo", $Repo, "--pattern", $Asset, "--output", $Destination, "--clobber")
    & gh @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "authenticated download failed for $Asset"
    }
}

function Get-OrbitBinaryVersion {
    param([Parameter(Mandatory)] [string] $Path)

    $output = & $Path --version 2>$null
    $versionText = ([string]::Join("`n", @($output))).Trim()
    $match = [regex]::Match(
        $versionText,
        '^(?:orbit )?v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)(?: \([^()\r\n]+\))?$'
    )
    if ($LASTEXITCODE -ne 0 -or -not $match.Success) {
        throw "$Path does not report a valid Orbit semantic version"
    }
    return $match.Groups[1].Value
}

function ConvertTo-SemVerParts {
    param([Parameter(Mandatory)] [string] $Version)

    if ($Version -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$') {
        throw "invalid semantic version: $Version"
    }
    return [pscustomobject]@{
        Major = [long]$Matches[1]
        Minor = [long]$Matches[2]
        Patch = [long]$Matches[3]
        Pre   = $Matches[4]
    }
}

function Compare-SemVer {
    param(
        [Parameter(Mandatory)] [string] $Left,
        [Parameter(Mandatory)] [string] $Right
    )

    $leftVersion = ConvertTo-SemVerParts $Left
    $rightVersion = ConvertTo-SemVerParts $Right
    foreach ($property in @("Major", "Minor", "Patch")) {
        if ($leftVersion.$property -gt $rightVersion.$property) { return 1 }
        if ($leftVersion.$property -lt $rightVersion.$property) { return -1 }
    }
    if (-not $leftVersion.Pre) {
        if ($rightVersion.Pre) { return 1 }
        return 0
    }
    if (-not $rightVersion.Pre) { return -1 }

    $leftIdentifiers = $leftVersion.Pre.Split(".")
    $rightIdentifiers = $rightVersion.Pre.Split(".")
    $count = [Math]::Max($leftIdentifiers.Count, $rightIdentifiers.Count)
    for ($index = 0; $index -lt $count; $index++) {
        if ($index -ge $leftIdentifiers.Count) { return -1 }
        if ($index -ge $rightIdentifiers.Count) { return 1 }
        $leftIdentifier = $leftIdentifiers[$index]
        $rightIdentifier = $rightIdentifiers[$index]
        if ($leftIdentifier -ceq $rightIdentifier) { continue }
        $leftNumber = 0L
        $rightNumber = 0L
        $leftNumeric = [long]::TryParse($leftIdentifier, [ref]$leftNumber)
        $rightNumeric = [long]::TryParse($rightIdentifier, [ref]$rightNumber)
        if ($leftNumeric -and $rightNumeric) {
            if ($leftNumber -gt $rightNumber) { return 1 }
            return -1
        }
        if ($leftNumeric) { return -1 }
        if ($rightNumeric) { return 1 }
        return [Math]::Sign([string]::CompareOrdinal($leftIdentifier, $rightIdentifier))
    }
    return 0
}

function Add-OrbitToUserPath {
    param([Parameter(Mandatory)] [string] $Directory)

    if ($env:ORBIT_SKIP_PATH_UPDATE -eq "1") { return }
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $entries = @($userPath -split ";" | Where-Object { $_ })
    if ($entries.Where({ $_.TrimEnd("\") -ieq $Directory.TrimEnd("\") }).Count -eq 0) {
        $updated = (@($entries) + $Directory) -join ";"
        [Environment]::SetEnvironmentVariable("Path", $updated, "User")
        Write-Host "Added $Directory to your user PATH."
    }

    $processEntries = @($env:Path -split ";" | Where-Object { $_ })
    if ($processEntries.Count -eq 0 -or $processEntries[0].TrimEnd("\") -ine $Directory.TrimEnd("\")) {
        $env:Path = (@($Directory) + $processEntries) -join ";"
    }
}

function Install-Orbit {
    $asset = Get-OrbitAsset
    $baseUrl = Get-ReleaseBaseUrl
    $directory = if ($env:ORBIT_INSTALL_DIR) {
        $env:ORBIT_INSTALL_DIR
    }
    else {
        Join-Path $env:LOCALAPPDATA "Programs\Orbit"
    }
    $null = New-Item -ItemType Directory -Force -Path $directory
    $target = Join-Path $directory "orbit.exe"
    $download = Join-Path $directory ".orbit-download.$PID.exe"
    $checksumFile = Join-Path $directory ".orbit-checksums.$PID.txt"

    try {
        Write-Host "Downloading $baseUrl/$asset"
        Receive-OrbitAsset -Asset $asset -Destination $download -Url "$baseUrl/$asset"
        Receive-OrbitAsset -Asset "checksums.txt" -Destination $checksumFile -Url "$baseUrl/checksums.txt"

        $checksumLine = Get-Content $checksumFile |
            Where-Object { $_ -match "^[0-9A-Fa-f]{64}\s+\*?$([regex]::Escape($asset))$" } |
            Select-Object -First 1
        if (-not $checksumLine) {
            throw "checksum missing for $asset"
        }
        $expected = ($checksumLine -split "\s+")[0].ToLowerInvariant()
        $actual = (Get-FileHash -Algorithm SHA256 -Path $download).Hash.ToLowerInvariant()
        if ($actual -ne $expected) {
            throw "checksum mismatch for $asset"
        }
        Write-Host "Verified SHA-256: $actual"

        Unblock-File -Path $download -ErrorAction SilentlyContinue
        $candidateVersion = Get-OrbitBinaryVersion $download
        if ($RequestedVersion -ne "latest" -and $RequestedVersion.TrimStart("v") -ne $candidateVersion) {
            throw "downloaded version $candidateVersion does not match requested $RequestedVersion"
        }

        if (Test-Path $target -PathType Leaf) {
            $currentVersion = $null
            try { $currentVersion = Get-OrbitBinaryVersion $target } catch { }
            if ($currentVersion -and
                (Compare-SemVer $currentVersion $candidateVersion) -gt 0 -and
                $env:ORBIT_ALLOW_DOWNGRADE -ne "1") {
                throw "refusing to replace newer Orbit $currentVersion with $candidateVersion; use 'orbit update --rollback', or set ORBIT_ALLOW_DOWNGRADE=1 for an intentional downgrade"
            }
            [System.IO.File]::Replace($download, $target, "$target.prev", $true)
            Write-Host "Previous binary backed up to $target.prev"
        }
        else {
            Move-Item -Path $download -Destination $target
        }

        Add-OrbitToUserPath $directory
        Write-Host "Installed: $target"
        & $target --version
        if (Get-Command orbit -ErrorAction SilentlyContinue) {
            Write-Host "Next: orbit init"
        }
        else {
            Write-Host "Next: & '$target' init"
        }
    }
    finally {
        Remove-Item -Force -ErrorAction SilentlyContinue $download, $checksumFile
    }
}

Install-Orbit
