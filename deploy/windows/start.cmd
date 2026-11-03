@echo off
setlocal
if not defined LOCALAPPDATA (
  echo ERROR: LOCALAPPDATA is unavailable. Run as a regular Windows user.
  exit /b 1
)
if not defined CODEARTS_PORT set "CODEARTS_PORT=10087"
echo CodeArts2API Panel
echo Open http://127.0.0.1:%CODEARTS_PORT%/ after startup.
echo Data: "%LOCALAPPDATA%\CodeArts2API"
echo First setup: read "state.json.setup.json" in the data folder.
echo Keep this window open. Press Ctrl+C to stop before upgrading or backing up.
"%~dp0codearts2api-panel.exe" -host 127.0.0.1 -port "%CODEARTS_PORT%" -data "%LOCALAPPDATA%\CodeArts2API\state.json" -no-import %*
set "PANEL_EXIT=%ERRORLEVEL%"
if not "%PANEL_EXIT%"=="0" pause
exit /b %PANEL_EXIT%
