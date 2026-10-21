@echo off
rem One-click build: produces build\windows-amd64\pingtunnel-workbench.exe
rem Requires Go 1.27+ and Bun. The bundled .toolchain\go is used when present.
setlocal
cd /d "%~dp0"

set "ROOT=%cd%"
set "APP=%ROOT%\pingtunnel-workbench"

if exist "%ROOT%\.toolchain\go\bin\go.exe" set "PATH=%ROOT%\.toolchain\go\bin;%PATH%"
if exist "%APPDATA%\npm" set "PATH=%PATH%;%APPDATA%\npm"

rem Go needs a build cache and a module proxy even when the shell has no
rem %LocalAppData% / %GOPROXY%; fill them in only when they are missing.
if not defined GOCACHE if defined LOCALAPPDATA set "GOCACHE=%LOCALAPPDATA%\go-build"
if not defined GOCACHE set "GOCACHE=%TEMP%\go-build"
if not defined LOCALAPPDATA set "LOCALAPPDATA=%TEMP%"
if not defined GOPATH if defined USERPROFILE set "GOPATH=%USERPROFILE%\go"
if not defined GOPROXY set "GOPROXY=https://goproxy.cn,direct"

where go >nul 2>nul
if errorlevel 1 (
  echo [x] go not found. Install Go 1.27+, or keep the bundled .toolchain\go folder.
  exit /b 1
)
where bun >nul 2>nul
if errorlevel 1 (
  echo [x] bun not found. Install it with: npm i -g bun
  exit /b 1
)

echo.
echo === toolchain ===
go version
call bun --version

cd /d "%APP%"
if not exist "node_modules" (
  echo.
  echo === bun install ===
  call bun install
  if errorlevel 1 exit /b 1
)

echo.
echo === mygo build ===
call bun run build
if errorlevel 1 exit /b 1

echo.
echo === runtime files ===
rem The tunnel client is not kept in the repository: fetch upstream's build the
rem first time, then leave it alone (replace bin\pingtunnel.exe by hand if your
rem server needs a different version).
if not exist "bin\pingtunnel.exe" (
  echo fetching pingtunnel client ...
  powershell -NoProfile -ExecutionPolicy Bypass -File "%ROOT%\scripts\fetch-client.ps1" -OutDir "%APP%\bin"
  if errorlevel 1 (
    echo [x] could not fetch the pingtunnel client. Drop your own pingtunnel.exe into pingtunnel-workbench\bin and run again.
    exit /b 1
  )
)

if exist "bin\pingtunnel.exe" copy /y "bin\pingtunnel.exe" "build\windows-amd64\pingtunnel.exe" >nul
if exist "bin\client-version.txt" copy /y "bin\client-version.txt" "build\windows-amd64\client-version.txt" >nul
if exist "bin\wintun.dll" copy /y "bin\wintun.dll" "build\windows-amd64\wintun.dll" >nul
if exist "bin\wintun-LICENSE.txt" copy /y "bin\wintun-LICENSE.txt" "build\windows-amd64\wintun-LICENSE.txt" >nul
if exist "bin\wintun.dll" if not exist "build\windows-amd64\wintun.dll" (
  echo [x] wintun.dll missing from the build output - TUN mode will not start.
)

echo.
echo === done ===
dir /b "build\windows-amd64"
echo.
echo Run: %APP%\build\windows-amd64\pingtunnel-workbench.exe  (as administrator)
endlocal
