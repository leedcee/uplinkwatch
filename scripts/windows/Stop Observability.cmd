@echo off
setlocal
title Stop UplinkWatch Observability
set "PROJECT_ROOT=%~dp0..\.."

echo Stopping Prometheus, Grafana, and Alertmanager...
docker compose -f "%PROJECT_ROOT%\deploy\docker-compose.yml" down

echo.
echo Observability services have stopped. Stored Docker data was preserved.
if /I not "%~1"=="--no-pause" pause
