# Remove the hidden per-user logon scheduled task for httpssh-relay.

[CmdletBinding()]
param(
  [string]$TaskName = "httpssh-relay-logon",
  [string]$InstallDir = (Join-Path $env:LOCALAPPDATA "httpssh"),
  [switch]$RemoveFiles
)

$ErrorActionPreference = "Stop"

function Stop-RelayProcessByPath {
  param(
    [Parameter(Mandatory = $true)]
    [string]$BinaryPath
  )

  if (-not (Test-Path -LiteralPath $BinaryPath)) {
    return
  }

  $resolvedPath = [IO.Path]::GetFullPath((Resolve-Path -LiteralPath $BinaryPath).Path)

  Get-CimInstance Win32_Process -Filter "name = 'httpssh-relay.exe'" |
    Where-Object {
      $_.ExecutablePath -and
      ([IO.Path]::GetFullPath($_.ExecutablePath) -ieq $resolvedPath)
    } |
    ForEach-Object {
      Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
      Write-Host "Stopped process $($_.ProcessId): $resolvedPath"
    }
}

if (-not $env:LOCALAPPDATA -and -not $InstallDir) {
  throw "LOCALAPPDATA is not set."
}

$installDirFull = [IO.Path]::GetFullPath($InstallDir)
$targetBinary = Join-Path $installDirFull "httpssh-relay.exe"

$existingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($existingTask) {
  Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
  Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
  Write-Host "Removed scheduled task: $TaskName"
} else {
  Write-Host "Scheduled task not found: $TaskName"
}

Stop-RelayProcessByPath -BinaryPath $targetBinary

if (-not $RemoveFiles) {
  Write-Host "Kept install directory: $installDirFull"
  exit 0
}

if (Test-Path -LiteralPath $installDirFull) {
  Remove-Item -LiteralPath $installDirFull -Recurse -Force
  Write-Host "Removed install directory: $installDirFull"
}
