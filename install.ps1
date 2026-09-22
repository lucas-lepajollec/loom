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
$InstallDir = Join-Path $env:LocalAppData "Programs\Loom"
$BinName = "loom.exe"

# Architecture detection
$Arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLower()
if ($Arch -eq "arm64") {
    $AssetName = "loom-windows-arm.exe"
} else {
    $AssetName = "loom-windows.exe"
}

Write-Host "--> Detected architecture: $Arch -> asset: $AssetName" -ForegroundColor Cyan

$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

try {
    $DownloadUrl = "https://github.com/$Repo/releases/latest/download/$AssetName"
    $DestFile = Join-Path $TempDir $BinName

    Write-Host "--> Downloading $AssetName from $DownloadUrl..." -ForegroundColor Cyan
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $DestFile -UseBasicParsing

    if (-not (Test-Path $DestFile)) {
        throw "Failed to download binary from $DownloadUrl"
    }

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    $TargetExe = Join-Path $InstallDir $BinName
    Copy-Item -Path $DestFile -Destination $TargetExe -Force

    # Ensure InstallDir is in User PATH
    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($UserPath -notlike "*$InstallDir*") {
        Write-Host "--> Adding $InstallDir to user PATH..." -ForegroundColor Cyan
        [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
        $env:Path += ";$InstallDir"
    }

    Write-Host "--> Initializing Loom installation..." -ForegroundColor Cyan
    & $TargetExe install

    Write-Host ""
    Write-Host "==========================================================================" -ForegroundColor Green
    Write-Host "✓ Loom successfully installed at $TargetExe" -ForegroundColor Green
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
