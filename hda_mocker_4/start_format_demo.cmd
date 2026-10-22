@echo off
"%~dp0hda_mocker_4_v1.5.2.exe" --config "%~dp0presets\format_demo\config.yaml"
if errorlevel 1 pause
