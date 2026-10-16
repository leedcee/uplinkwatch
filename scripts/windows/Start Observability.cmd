@echo off
setlocal
title UplinkWatch Observability
set "PROJECT_ROOT=%~dp0..\.."
set "COMPOSE_FILE=%PROJECT_ROOT%\deploy\docker-compose.yml"

call :ensure_docker
if errorlevel 1 goto failed

echo Starting Prometheus, Grafana, and Alertmanager...
docker compose -f "%COMPOSE_FILE%" up -d
if errorlevel 1 goto failed

echo.
echo Observability services are running:
echo   Grafana:      http://127.0.0.1:3000
echo   Prometheus:   http://127.0.0.1:9090
echo   Alertmanager: http://127.0.0.1:9093

if /I not "%~1"=="--no-open" start "" "http://127.0.0.1:3000/d/uplinkwatch-overview/uplinkwatch-overview"
if /I not "%~2"=="--no-pause" pause
exit /b 0

:ensure_docker
docker info >nul 2>&1
if not errorlevel 1 exit /b 0

set "DOCKER_DESKTOP=C:\Program Files\Docker\Docker\Docker Desktop.exe"
if not exist "%DOCKER_DESKTOP%" (
  echo Docker Desktop is not installed or was not found.
  exit /b 1
)

echo Starting Docker Desktop. This can take about a minute...
start "" "%DOCKER_DESKTOP%"
for /L %%I in (1,1,60) do (
  timeout /t 2 /nobreak >nul
  docker info >nul 2>&1
  if not errorlevel 1 exit /b 0
)

echo Docker Desktop did not become ready in time.
exit /b 1

:failed
echo.
echo Unable to start the observability services.
if /I not "%~2"=="--no-pause" pause
exit /b 1
