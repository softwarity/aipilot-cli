#Requires -Version 5.1
<#
.SYNOPSIS
    AIPilot CLI Installer for Windows
.DESCRIPTION
    Downloads and installs the AIPilot CLI to %LOCALAPPDATA%\Programs\aipilot
    and adds it to the user's PATH.
.EXAMPLE
    irm https://raw.githubusercontent.com/softwarity/aipilot-cli/main/install.ps1 | iex
#>

$ErrorActionPreference = "Stop"
trap { Write-Host "`n$_" -ForegroundColor Red; Read-Host "Press Enter to exit"; exit 1 }

$Repo = "softwarity/aipilot-cli"
$BinaryName = "aipilot-cli.exe"
$InstallDir = Join-Path $env:LOCALAPPDATA "Programs\aipilot"

function Write-Info { param($Message) Write-Host "[INFO] " -ForegroundColor Green -NoNewline; Write-Host $Message }
function Write-Warn { param($Message) Write-Host "[WARN] " -ForegroundColor Yellow -NoNewline; Write-Host $Message }
function Write-Err { param($Message) Write-Host "[ERROR] " -ForegroundColor Red -NoNewline; Write-Host $Message; exit 1 }

function Get-Architecture {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64" { return "amd64" }
        "Arm64" { return "arm64" }
        default { Write-Err "Unsupported architecture: $arch" }
    }
}

function Get-LatestVersion {
    $releases = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
    return $releases.tag_name
}

function Add-ToPath {
    param($Directory)

    $currentPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($currentPath -notlike "*$Directory*") {
        $newPath = "$Directory;$currentPath"
        [Environment]::SetEnvironmentVariable("PATH", $newPath, "User")
        $env:PATH = "$Directory;$env:PATH"
        Write-Info "Added $Directory to user PATH"
        return $true
    }
    return $false
}

function Main {
    Write-Host ""
    Write-Host "=======================================" -ForegroundColor Cyan
    Write-Host "      AIPilot CLI Installer            " -ForegroundColor Cyan
    Write-Host "=======================================" -ForegroundColor Cyan
    Write-Host ""

    # Detect architecture
    $arch = Get-Architecture
    Write-Info "Detected: windows/$arch"

    # Get latest version
    Write-Info "Fetching latest version..."
    $version = Get-LatestVersion
    if (-not $version) {
        Write-Err "Failed to fetch latest version"
    }
    Write-Info "Latest version: $version"

    # Build download URL
    $downloadUrl = "https://github.com/$Repo/releases/download/$version/aipilot-cli-windows-$arch.exe"
    Write-Info "Downloading from: $downloadUrl"

    # Create install directory
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    # Download binary
    $binaryPath = Join-Path $InstallDir $BinaryName
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $binaryPath -UseBasicParsing
    }
    catch {
        Write-Err "Failed to download binary: $_"
    }

    Write-Info "Installed to: $binaryPath"

    # Add to PATH
    $pathAdded = Add-ToPath -Directory $InstallDir
    if ($pathAdded) {
        Write-Host ""
        Write-Warn "PATH was updated. Please restart your terminal for changes to take effect."
    }

    # Verify installation
    Write-Host ""
    Write-Info "Installation complete! ✓"
    Write-Host ""
    Write-Host "Run 'aipilot-cli' to start"
    Write-Host ""

    if ($pathAdded) {
        Write-Host "Note: You may need to restart your terminal or run:" -ForegroundColor Yellow
        Write-Host "  `$env:PATH = [Environment]::GetEnvironmentVariable('PATH', 'User') + ';' + [Environment]::GetEnvironmentVariable('PATH', 'Machine')" -ForegroundColor Gray
        Write-Host ""
    }
}

Main
