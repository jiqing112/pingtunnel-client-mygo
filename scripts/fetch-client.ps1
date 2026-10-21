# fetch-client.ps1 — downloads the pingtunnel client binary from upstream
# releases and drops it where the app expects to find it.
#
#   .\scripts\fetch-client.ps1                       # latest, into pingtunnel-workbench\bin
#   .\scripts\fetch-client.ps1 -Tag 2.4              # a pinned version
#   .\scripts\fetch-client.ps1 -OutDir build\win     # somewhere else
param(
  [string]$Tag = "",
  [string]$OutDir = "pingtunnel-workbench\bin"
)

$ErrorActionPreference = "Stop"

$repo = "esrrhs/pingtunnel"
if ($Tag) {
  $release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/tags/$Tag" -Headers @{ "User-Agent" = "pingtunnel-workbench" }
} else {
  $release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest" -Headers @{ "User-Agent" = "pingtunnel-workbench" }
}

$asset = $release.assets | Where-Object { $_.name -match "windows_amd64.*\.zip$" } | Select-Object -First 1
if (-not $asset) {
  throw "release $($release.tag_name) has no windows_amd64 asset; pass -Tag with a version that ships one"
}

$tmp = Join-Path $env:TEMP ("pt-client-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
  $zip = Join-Path $tmp "client.zip"
  Write-Host "downloading $($asset.name) from $($release.tag_name) ..."
  Invoke-WebRequest $asset.browser_download_url -OutFile $zip -UseBasicParsing
  Expand-Archive -Path $zip -DestinationPath $tmp -Force

  $exe = Get-ChildItem $tmp -Recurse -Filter "pingtunnel.exe" | Select-Object -First 1
  if (-not $exe) { throw "pingtunnel.exe not found inside $($asset.name)" }

  New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
  Copy-Item $exe.FullName (Join-Path $OutDir "pingtunnel.exe") -Force
  Set-Content -Path (Join-Path $OutDir "client-version.txt") -Value $release.tag_name -Encoding ascii -NoNewline

  Write-Host "ok: $($release.tag_name) -> $OutDir\pingtunnel.exe"
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
