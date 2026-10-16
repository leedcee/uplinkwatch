@echo off
setlocal
title UplinkWatch Monitor
set "PROJECT_ROOT=%~dp0..\.."

echo Starting the UplinkWatch agent and local dashboard...
echo Keep this window open. Press Ctrl+C to stop the monitor.
echo.

set "MONITOR_CONFIG=%PROJECT_ROOT%\data\sites.json"
set "MONITOR_EXE=%PROJECT_ROOT%\bin\uplinkwatch.exe"

if not exist "%MONITOR_EXE%" (
  echo UplinkWatch has not been built yet.
  echo Run: go build -o bin\uplinkwatch.exe .\cmd\uplinkwatch
  pause
  exit /b 1
)

if not exist "%MONITOR_CONFIG%" (
  echo No site list was found. Building one from supported browsers...
  call "%~dp0Refresh Sites from Browsers.cmd" --no-pause
  if errorlevel 1 goto failed
)

"%MONITOR_EXE%" -config "%MONITOR_CONFIG%" -open-browser

echo.
echo The monitor has stopped.
pause
exit /b 0

:failed
echo.
echo The monitor could not create a site list.
pause
exit /b 1
