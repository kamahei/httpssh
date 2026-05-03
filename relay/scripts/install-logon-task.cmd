@echo off
setlocal

set "TASK_NAME=httpssh-relay-logon"
set "INSTALL_DIR=%LOCALAPPDATA%\httpssh"
set "SCRIPT_DIR=%~dp0"

for %%I in ("%SCRIPT_DIR%..") do set "RELAY_DIR=%%~fI"

if "%~1"=="/?" goto usage
if "%~1"=="-h" goto usage
if "%~1"=="--help" goto usage

if not "%~2"=="" (
  echo Unexpected argument: "%~2"
  goto usage_error
)

if "%LOCALAPPDATA%"=="" (
  echo LOCALAPPDATA is not set. Run this from an interactive user session.
  exit /b 1
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
set "INSTALL_SCRIPT=%SCRIPT_DIR%install-logon-task.ps1"
set "UNINSTALL_SCRIPT=%SCRIPT_DIR%uninstall-logon-task.ps1"

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

if not exist "%UNINSTALL_SCRIPT%" (
  echo Uninstall script not found: %UNINSTALL_SCRIPT%
  exit /b 1
)

if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"
if errorlevel 1 exit /b 1

echo Stopping existing %TASK_NAME% task, if present...
%PS_EXE% -NoProfile -ExecutionPolicy Bypass -File "%UNINSTALL_SCRIPT%" -TaskName "%TASK_NAME%" -InstallDir "%INSTALL_DIR%"
if errorlevel 1 exit /b 1

echo Copying relay files to %INSTALL_DIR%...
copy /Y "%SOURCE_BINARY%" "%TARGET_BINARY%" >nul
if errorlevel 1 exit /b 1

copy /Y "%SOURCE_CONFIG%" "%TARGET_CONFIG%" >nul
if errorlevel 1 exit /b 1

echo Registering %TASK_NAME% logon task for the current user...
%PS_EXE% -NoProfile -ExecutionPolicy Bypass -File "%INSTALL_SCRIPT%" -Binary "%TARGET_BINARY%" -Config "%TARGET_CONFIG%" -TaskName "%TASK_NAME%"
if errorlevel 1 exit /b 1

echo Done.
echo The relay runs hidden at logon. Logs are written to:
echo   %INSTALL_DIR%\logs\httpssh-relay.log
exit /b 0

:usage
echo Usage: %~nx0 [path-to-httpssh-relay.exe]
echo.
echo If no exe path is provided, this script uses:
echo   %RELAY_DIR%\dist\httpssh-relay.exe
echo.
echo The config is always copied from:
echo   %RELAY_DIR%\config.yaml
echo.
echo The current user logon task installs files under:
echo   %LOCALAPPDATA%\httpssh
exit /b 0

:usage_error
echo Usage: %~nx0 [path-to-httpssh-relay.exe]
exit /b 1
