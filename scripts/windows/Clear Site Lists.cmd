@echo off
setlocal
title Clear Generated Site Lists
set "PROJECT_ROOT=%~dp0..\.."

echo Clearing generated browser site lists...
echo.
if exist "%PROJECT_ROOT%\data\sites.json" del /q "%PROJECT_ROOT%\data\sites.json"

echo The generated site list has been removed.
echo Start the monitor to build a fresh list.
pause
