@echo off
setlocal
if not exist "%~dp0NVIDIAReplayMonitor.exe" (
  echo NVIDIAReplayMonitor.exe was not found in this folder.
  pause
  exit /b 1
)
start "" /wait "%~dp0NVIDIAReplayMonitor.exe" --uninstall
set "result=%errorlevel%"
echo.
if not "%result%"=="0" (
  echo Uninstall did not complete. Keep this folder and the settings backup.
) else (
  echo Uninstall completed. You can delete NVIDIAReplayMonitor.exe, uninstall.cmd and README.md.
)
pause
exit /b %result%
