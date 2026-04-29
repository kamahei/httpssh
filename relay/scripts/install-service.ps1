# Install httpssh-relay as a Windows service.
#
# Usage (from an elevated PowerShell):
#   pwsh -File scripts/install-service.ps1
#   pwsh -File scripts/install-service.ps1 -Binary "C:\httpssh\httpssh-relay.exe" -Config "C:\httpssh\config.yaml"
#
# Defaults: binary at <relay-dir>/httpssh-relay.exe, config at <relay-dir>/config.yaml.
# Both paths must be absolute when the service starts; the script resolves them for you.

[CmdletBinding()]
param(
  [string]$Binary,
  [string]$Config,
  [string]$Name = "httpssh-relay",
  [string]$DisplayName = "httpssh relay",
  [string]$Description = "PowerShell HTTP/WebSocket relay for httpssh"
)

$ErrorActionPreference = "Stop"

if (-not (([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator))) {
  throw "This script must be run from an elevated PowerShell session."
}

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
if (-not $Binary) { $Binary = Join-Path $repoRoot "httpssh-relay.exe" }
if (-not $Config) { $Config = Join-Path $repoRoot "config.yaml" }

$Binary = (Resolve-Path $Binary).Path
if (-not (Test-Path $Binary)) { throw "Binary not found: $Binary" }
if (-not (Test-Path $Config)) {
  Write-Warning "Config file does not exist at $Config; the service will start with a generated bearer (logs unattended)."
}

$existing = Get-Service -Name $Name -ErrorAction SilentlyContinue
if ($existing) {
  Write-Host "Service '$Name' already exists; stopping and removing first..."
  if ($existing.Status -eq "Running") { Stop-Service -Name $Name -Force }
  & sc.exe delete $Name | Out-Null
  Start-Sleep -Seconds 1
}

# Quote the executable path; embed --config so the service starts under
# its own working directory without needing a wrapper.
$cmdline = "`"$Binary`" --config `"$Config`""

Write-Host "Creating service '$Name' -> $cmdline"
& sc.exe create $Name binPath= $cmdline DisplayName= $DisplayName start= auto | Out-Null
& sc.exe description $Name $Description | Out-Null

# Recovery: restart on failure, twice, then leave it.
& sc.exe failure $Name reset= 86400 actions= restart/5000/restart/15000/`"`" | Out-Null

Write-Host "Service installed. Start with: Start-Service $Name"
