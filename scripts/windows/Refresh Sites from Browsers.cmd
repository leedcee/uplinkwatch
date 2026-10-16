@echo off
setlocal
title Refresh Sites from Browsers
set "PROJECT_ROOT=%~dp0..\.."
set "MONITOR_EXE=%PROJECT_ROOT%\bin\uplinkwatch.exe"

echo Reading domain-level visit counts from Arc, Chrome, Edge, Brave, and Firefox...
echo Page paths, titles, and search queries will not be saved.
echo.
if not exist "%MONITOR_EXE%" (
  echo UplinkWatch has not been built yet.
  if /I not "%~1"=="--no-pause" pause
  exit /b 1
)

"%MONITOR_EXE%" -discover-browsers -discovery-output "%PROJECT_ROOT%\data\sites.json" -discovery-limit 30 -discovery-days 180
if errorlevel 1 goto failed

echo.
echo The site list has been updated. Restart the monitor to use it.
if /I not "%~1"=="--no-pause" pause
exit /b 0

:failed
echo.
echo Update failed. Close all browsers and run this file again.
if /I not "%~1"=="--no-pause" pause
exit /b 1
