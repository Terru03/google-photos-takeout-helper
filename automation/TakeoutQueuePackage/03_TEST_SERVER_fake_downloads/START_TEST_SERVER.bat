@echo off
title Fake Takeout Test Server
cd /d "%~dp0"
python takeout_test_server.py
pause
