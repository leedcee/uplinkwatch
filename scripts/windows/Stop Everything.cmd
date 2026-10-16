@echo off
setlocal
title Stop UplinkWatch
set "PROJECT_ROOT=%~dp0..\.."

echo Stopping the UplinkWatch agent...
taskkill /IM uplinkwatch.exe /T /F >nul 2>&1

echo Stopping Prometheus, Grafana, and Alertmanager...
docker compose -f "%PROJECT_ROOT%\deploy\docker-compose.yml" down

echo.
echo All UplinkWatch components have stopped. Stored Docker data was preserved.
if /I not "%~1"=="--no-pause" pause
