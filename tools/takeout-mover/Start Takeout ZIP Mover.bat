@echo off
title Takeout ZIP Mover
cd /d "%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0TakeoutZipMover.ps1"
pause
