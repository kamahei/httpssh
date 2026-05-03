# Install httpssh-relay as a hidden per-user logon scheduled task.
#
# This is intentionally a per-user task, not a Windows service. It avoids a
# stored service password and lets the relay inherit the logged-on user's
# environment.

[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string]$Binary,

  [Parameter(Mandatory = $true)]
  [string]$Config,

  [string]$TaskName = "httpssh-relay-logon",
  [string]$Description = "Starts httpssh relay when the current user logs on.",
  [switch]$NoStart
)

$ErrorActionPreference = "Stop"

function Resolve-RequiredPath {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Path,

    [Parameter(Mandatory = $true)]
    [string]$Label
  )

  if (-not (Test-Path -LiteralPath $Path)) {
    throw "$Label not found: $Path"
  }

  return (Resolve-Path -LiteralPath $Path).Path
}

$Binary = Resolve-RequiredPath -Path $Binary -Label "Binary"
$Config = Resolve-RequiredPath -Path $Config -Label "Config"

$installDir = Split-Path -Parent $Binary
$runScript = Join-Path $installDir "run-logon-task.cmd"
$launcherScript = Join-Path $installDir "run-logon-task.vbs"
$logDir = Join-Path $installDir "logs"

New-Item -ItemType Directory -Force -Path $logDir | Out-Null

$runScriptContent = @'
@echo off
setlocal
cd /d "%~dp0"
if not exist "logs" mkdir "logs"
"httpssh-relay.exe" --config "config.yaml" >> "logs\httpssh-relay.log" 2>&1
'@

Set-Content -LiteralPath $runScript -Value $runScriptContent -Encoding ASCII

$launcherContent = @'
Option Explicit

Dim shell, fso, scriptDir, runScript

Set shell = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")
scriptDir = fso.GetParentFolderName(WScript.ScriptFullName)
runScript = fso.BuildPath(scriptDir, "run-logon-task.cmd")

WScript.Quit shell.Run("""" & runScript & """", 0, True)
'@

Set-Content -LiteralPath $launcherScript -Value $launcherContent -Encoding ASCII

$identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$windowsDir = if ($env:SystemRoot) { $env:SystemRoot } else { $env:WINDIR }
if (-not $windowsDir) {
  throw "SystemRoot is not set."
}

$wscript = Join-Path $windowsDir "System32\wscript.exe"
if (-not (Test-Path -LiteralPath $wscript)) {
  throw "wscript.exe not found: $wscript"
}

$existingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($existingTask) {
  Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
  Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}

$action = New-ScheduledTaskAction -Execute $wscript -Argument "`"$launcherScript`""
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet `
  -MultipleInstances IgnoreNew `
  -StartWhenAvailable `
  -AllowStartIfOnBatteries `
  -DontStopIfGoingOnBatteries `
  -Hidden `
  -ExecutionTimeLimit (New-TimeSpan -Seconds 0)

$task = New-ScheduledTask `
  -Action $action `
  -Trigger $trigger `
  -Principal $principal `
  -Settings $settings `
  -Description $Description

Register-ScheduledTask -TaskName $TaskName -InputObject $task | Out-Null

$service = Get-Service -Name "httpssh-relay" -ErrorAction SilentlyContinue
if ($service -and $service.Status -eq "Running") {
  Write-Warning "The httpssh-relay Windows service is running. Stop or uninstall it before starting the logon task to avoid port conflicts."
  Write-Host "Registered scheduled task: $TaskName"
  exit 0
}

if (-not $NoStart) {
  Start-ScheduledTask -TaskName $TaskName
  Write-Host "Registered and started scheduled task: $TaskName"
} else {
  Write-Host "Registered scheduled task: $TaskName"
}

Write-Host "Task user: $identity"
Write-Host "Log file: $(Join-Path $logDir 'httpssh-relay.log')"
