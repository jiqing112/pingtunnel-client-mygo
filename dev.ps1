# dev.ps1 — puts the local Go toolchain, Bun and the Go module proxy on PATH
# for this shell. Dot-source it: . .\dev.ps1

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path

if (-not $env:APPDATA) { $env:APPDATA = Join-Path $env:USERPROFILE 'AppData\Roaming' }
if (-not $env:LOCALAPPDATA) { $env:LOCALAPPDATA = Join-Path $env:USERPROFILE 'AppData\Local' }

$env:GOPROXY = 'https://goproxy.cn,direct'
$env:GOSUMDB = 'sum.golang.google.cn'
$env:GOCACHE = Join-Path $env:LOCALAPPDATA 'go-build'
$env:GOPATH = Join-Path $env:USERPROFILE 'go'
$env:PATH = "$Root\.toolchain\go\bin;$env:APPDATA\npm;$env:PATH"
