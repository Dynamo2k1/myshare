# Install MyShare on Windows without administrator rights.
#
#   powershell -ExecutionPolicy Bypass -File scripts\install.ps1
#
# Installs to %LOCALAPPDATA%\Programs\MyShare and adds that directory to the
# per-user PATH. Builds from source if a prebuilt bin\myshare.exe is absent.

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$binName = "myshare.exe"
$installDir = Join-Path $env:LOCALAPPDATA "Programs\MyShare"
$dest = Join-Path $installDir $binName

# --- locate or build ----------------------------------------------------
$src = Join-Path $repoRoot "bin\$binName"
if (-not (Test-Path $src)) {
    if (Get-Command go -ErrorAction SilentlyContinue) {
        Write-Host "Building $binName from source..."
        if ((Test-Path "web\node_modules") -or (Get-Command npm -ErrorAction SilentlyContinue)) {
            Push-Location web
            if (-not (Test-Path node_modules)) { npm ci }
            try { npm run build } catch { Write-Host "  (frontend build skipped; using stub)" }
            Pop-Location
        }
        $version = (git describe --tags --always --dirty 2>$null); if (-not $version) { $version = "dev" }
        $env:CGO_ENABLED = "0"
        New-Item -ItemType Directory -Force -Path "bin" | Out-Null
        go build -trimpath -ldflags "-s -w -X main.version=$version" -o $src ./cmd/myshare
    } else {
        Write-Error "No prebuilt bin\$binName and Go is not installed."
    }
}

# --- install ----------------------------------------------------------
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Copy-Item -Force $src $dest
Write-Host "Installed $dest"
& $dest --version

# --- per-user PATH -------------------------------------------------
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
    Write-Host "Added $installDir to your user PATH. Open a new terminal to pick it up."
}

Write-Host ""
Write-Host "Run it:    myshare --port 8787 --data-dir `"$env:USERPROFILE\MyShare`""
Write-Host "Autostart: myshare service install"
