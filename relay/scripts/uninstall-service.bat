@echo off
setlocal

set "SERVICE_NAME=httpssh-relay"
set "INSTALL_DIR=C:\Program Files\httpssh"
set "SCRIPT_DIR=%~dp0"
set "TARGET_BINARY=%INSTALL_DIR%\httpssh-relay.exe"
set "TARGET_CONFIG=%INSTALL_DIR%\config.yaml"
set "UNINSTALL_SCRIPT=%SCRIPT_DIR%uninstall-service.ps1"

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

if not exist "%UNINSTALL_SCRIPT%" (
  echo Uninstall script not found: %UNINSTALL_SCRIPT%
  exit /b 1
)

echo Uninstalling %SERVICE_NAME% service...
%PS_EXE% -NoProfile -ExecutionPolicy Bypass -File "%UNINSTALL_SCRIPT%" -Name "%SERVICE_NAME%"
if errorlevel 1 exit /b 1

if exist "%TARGET_BINARY%" (
  echo Removing %TARGET_BINARY%...
  del /F /Q "%TARGET_BINARY%"
  if exist "%TARGET_BINARY%" (
    echo Failed to remove %TARGET_BINARY%.
    exit /b 1
  )
)

if exist "%TARGET_CONFIG%" (
  echo Removing %TARGET_CONFIG%...
  del /F /Q "%TARGET_CONFIG%"
  if exist "%TARGET_CONFIG%" (
    echo Failed to remove %TARGET_CONFIG%.
    exit /b 1
  )
)

rmdir "%INSTALL_DIR%" 2>nul

echo Done.
exit /b 0
