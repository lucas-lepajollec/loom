# ==============================================================================
# Loom Installer (Windows PowerShell)
# ==============================================================================
# Installs Loom — Workstation control plane and test bench for llama.cpp.
#
# Usage:
#   irm https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.ps1 | iex
# ==============================================================================

$ErrorActionPreference = "Stop"

$Repo = "lucas-lepajollec/loom"
$BinName = "loom.exe"

# Architecture detection
$Arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLower()
if ($Arch -eq "arm64") {
    $AssetName = "loom-windows-arm.exe"
} elseif ($Arch -eq "x64") {
    $AssetName = "loom-windows.exe"
} else {
    throw "Unsupported Windows architecture: $Arch"
}

Write-Host "--> Detected architecture: $Arch -> asset: $AssetName" -ForegroundColor Cyan

$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

try {
    $ReleasePath = "latest/download"
    if ($env:LOOM_VERSION) {
        if ($env:LOOM_VERSION -notmatch '^v\d+\.\d+\.\d+$') {
            throw "LOOM_VERSION must be a release tag such as v0.1.0"
        }
        $ReleasePath = "download/$($env:LOOM_VERSION)"
    }
    $ReleaseBase = "https://github.com/$Repo/releases/$ReleasePath"
    $DownloadUrl = "$ReleaseBase/$AssetName"
    $DestFile = Join-Path $TempDir $BinName
    $ChecksumFile = Join-Path $TempDir "SHA256SUMS.txt"

    Write-Host "--> Downloading $AssetName from $DownloadUrl..." -ForegroundColor Cyan
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $DestFile -UseBasicParsing
    Invoke-WebRequest -Uri "$ReleaseBase/SHA256SUMS.txt" -OutFile $ChecksumFile -UseBasicParsing
    if (-not (Test-Path $DestFile) -or (Get-Item $DestFile).Length -eq 0) {
        throw "Release binary is missing or empty: $AssetName"
    }
    $AssetPattern = [regex]::Escape($AssetName)
    $ChecksumLine = Get-Content $ChecksumFile | Where-Object { $_ -match "^[a-fA-F0-9]{64}\s+\*?$AssetPattern$" } | Select-Object -First 1
    if (-not $ChecksumLine) {
        throw "No SHA-256 entry for $AssetName in the release manifest"
    }
    $ExpectedHash = ($ChecksumLine -split '\s+')[0]
    $ActualHash = (Get-FileHash -Algorithm SHA256 -Path $DestFile).Hash
    if ($ActualHash -ne $ExpectedHash) {
        throw "SHA-256 mismatch for $AssetName; installation cancelled"
    }

    Write-Host "--> Initializing Loom installation..." -ForegroundColor Cyan
    # Loom itself copies this verified binary into LOOM_HOME\bin and adds that
    # directory to the user PATH. A second copy would become stale.
    & $DestFile install
    if ($LASTEXITCODE -ne 0) {
        throw "loom install failed with exit code $LASTEXITCODE"
    }

    Write-Host ""
    Write-Host "==========================================================================" -ForegroundColor Green
    Write-Host "Loom installed. Open a new terminal and run 'loom where' to inspect its paths." -ForegroundColor Green
    Write-Host "==========================================================================" -ForegroundColor Green
    Write-Host "To start the web interface:"
    Write-Host "   loom web 8091"
    Write-Host "=========================================================================="
}
finally {
    if (Test-Path $TempDir) {
        Remove-Item -Path $TempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
