@echo off
setlocal enabledelayedexpansion
set "LIB=%~dp0lib"

echo ============================================
echo   OPC UA offline dependencies installer
echo   wheel dir : %LIB%
echo.
python --version
echo ============================================
echo.

rem cmd does not expand wildcards; collect all wheels via for loop
set "WHEELS="
for %%w in ("%LIB%\*.whl") do set "WHEELS=!WHEELS! "%%w""

echo Installing %WHEELS% packages...
python -m pip install --no-index --find-links="%LIB%" %WHEELS%

echo.
echo Done.
pause
