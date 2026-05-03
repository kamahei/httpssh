@echo off
setlocal

set "SERVICE_NAME=httpssh-relay"
set "INSTALL_DIR=C:\Program Files\httpssh"
set "SCRIPT_DIR=%~dp0"

for %%I in ("%SCRIPT_DIR%..") do set "RELAY_DIR=%%~fI"

if "%~1"=="/?" goto usage
if "%~1"=="-h" goto usage
if "%~1"=="--help" goto usage

if not "%~2"=="" (
  echo Unexpected argument: "%~2"
  goto usage_error
)

if "%~1"=="" (
  set "SOURCE_BINARY=%RELAY_DIR%\dist\httpssh-relay.exe"
) else (
  set "SOURCE_BINARY=%~1"
)

for %%I in ("%SOURCE_BINARY%") do set "SOURCE_BINARY=%%~fI"

set "SOURCE_CONFIG=%RELAY_DIR%\config.yaml"
set "TARGET_BINARY=%INSTALL_DIR%\httpssh-relay.exe"
set "TARGET_CONFIG=%INSTALL_DIR%\config.yaml"
set "INSTALL_SCRIPT=%SCRIPT_DIR%install-service.ps1"

fltmc >nul 2>&1
if errorlevel 1 (
  echo This script must be run from an elevated Command Prompt or PowerShell.
  exit /b 1
)

where pwsh.exe >nul 2>&1
if errorlevel 1 (
  set "PS_EXE=powershell.exe"
) else (
  set "PS_EXE=pwsh.exe"
)

if not exist "%SOURCE_BINARY%" (
  echo Relay binary not found: %SOURCE_BINARY%
  echo Build it first with: go build -trimpath -o dist\httpssh-relay.exe .\cmd\httpssh-relay
  exit /b 1
)

if not exist "%SOURCE_CONFIG%" (
  echo Relay config not found: %SOURCE_CONFIG%
  exit /b 1
)

if not exist "%INSTALL_SCRIPT%" (
  echo Install script not found: %INSTALL_SCRIPT%
  exit /b 1
)

sc.exe query "%SERVICE_NAME%" >nul 2>&1
if not errorlevel 1 (
  echo Stopping existing service, if running...
  %PS_EXE% -NoProfile -ExecutionPolicy Bypass -Command "$svc = Get-Service -Name '%SERVICE_NAME%' -ErrorAction SilentlyContinue; if ($svc -and $svc.Status -ne 'Stopped') { Stop-Service -Name '%SERVICE_NAME%' -Force; $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30)) }"
  if errorlevel 1 exit /b 1
)

if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"
if errorlevel 1 exit /b 1

echo Copying relay files to %INSTALL_DIR%...
copy /Y "%SOURCE_BINARY%" "%TARGET_BINARY%" >nul
if errorlevel 1 exit /b 1

copy /Y "%SOURCE_CONFIG%" "%TARGET_CONFIG%" >nul
if errorlevel 1 exit /b 1

echo Installing %SERVICE_NAME% service...
%PS_EXE% -NoProfile -ExecutionPolicy Bypass -File "%INSTALL_SCRIPT%" -Binary "%TARGET_BINARY%" -Config "%TARGET_CONFIG%" -Name "%SERVICE_NAME%"
if errorlevel 1 exit /b 1

echo Starting %SERVICE_NAME% service...
%PS_EXE% -NoProfile -ExecutionPolicy Bypass -Command "Start-Service -Name '%SERVICE_NAME%'; Get-Service -Name '%SERVICE_NAME%'"
if errorlevel 1 exit /b 1

echo Done.
exit /b 0

:usage
echo Usage: %~nx0 [path-to-httpssh-relay.exe]
echo.
echo If no exe path is provided, this script uses:
echo   %RELAY_DIR%\dist\httpssh-relay.exe
echo.
echo The config is always copied from:
echo   %RELAY_DIR%\config.yaml
exit /b 0

:usage_error
echo Usage: %~nx0 [path-to-httpssh-relay.exe]
exit /b 1
