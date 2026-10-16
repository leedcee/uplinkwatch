@echo off
setlocal
title UplinkWatch Complete Stack
set "PROJECT_ROOT=%~dp0..\.."

set "MONITOR_CONFIG=%PROJECT_ROOT%\data\sites.json"
set "MONITOR_EXE=%PROJECT_ROOT%\bin\uplinkwatch.exe"

if not exist "%MONITOR_EXE%" goto missing_binary
if not exist "%MONITOR_CONFIG%" (
  call "%~dp0Refresh Sites from Browsers.cmd" --no-pause
  if errorlevel 1 goto failed
)

call "%~dp0Start Observability.cmd" --no-open --no-pause
if errorlevel 1 goto failed

echo.
echo Starting the UplinkWatch agent...
echo The local dashboard and Grafana will open in your browser.
echo Use Stop Everything.cmd to stop all components.
set "OPEN_BROWSER=-open-browser"
if /I "%~1"=="--no-open" set "OPEN_BROWSER="
if /I not "%~1"=="--no-open" start "" "http://127.0.0.1:3000/d/uplinkwatch-overview/uplinkwatch-overview"
"%MONITOR_EXE%" -config "%MONITOR_CONFIG%" -listen-address "0.0.0.0:9876" %OPEN_BROWSER%
exit /b 0

:missing_binary
echo.
echo UplinkWatch has not been built yet.
echo Run: go build -o bin\uplinkwatch.exe .\cmd\uplinkwatch
if /I not "%~2"=="--no-pause" pause
exit /b 1

:failed
echo.
echo The complete stack could not be started.
if /I not "%~2"=="--no-pause" pause
exit /b 1
