<#
.SYNOPSIS
  Publish the built Roxy TV APK to Roxy Server so phones and TVs offer the update.

.DESCRIPTION
  Copies app/build/outputs/apk/<variant>/app-<variant>.apk into <server data>/apps/tv/
  and updates latest.json there (keyed by applicationId, so debug and release builds
  are tracked separately). The server serves it at GET /v1/apps/tv/latest and
  /v1/apps/tv/download; the app compares versionCode with its own.

  The default is the perf build (gradlew :app:assemblePerf): the debug app's id, key and
  sign-in, built the release way. A plain debug build is debuggable, so Android never
  compiles it ahead of time and the TV feels laggy; do not ship that one.

.EXAMPLE
  .\tools\publish-apk.ps1 -Notes "Downloads, autoplay next episode"
  .\tools\publish-apk.ps1 -Variant release -DataDir \\server\roxy\data
#>
param(
    [ValidateSet("perf", "debug", "release")] [string] $Variant = "perf",
    [string] $DataDir = (Join-Path $PSScriptRoot "..\..\..\server\data"),
    [string] $Notes = ""
)
$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..")
$gradle = Get-Content (Join-Path $root "app\build.gradle.kts") -Raw
$code = [int]([regex]::Match($gradle, 'versionCode\s*=\s*(\d+)').Groups[1].Value)
$name = [regex]::Match($gradle, 'versionName\s*=\s*"([^"]+)"').Groups[1].Value
$appId = "tv.roxy.tv" + $(if ($Variant -eq "release") { "" } else { ".debug" })
$apk = Join-Path $root "app\build\outputs\apk\$Variant\app-$Variant.apk"
if (-not (Test-Path $apk)) { throw "APK not found: $apk (build first)" }

$dir = Join-Path $DataDir "apps\tv"
New-Item -ItemType Directory -Force $dir | Out-Null
$file = "roxy-tv-$name-$Variant.apk"
Copy-Item $apk (Join-Path $dir $file) -Force

$latestPath = Join-Path $dir "latest.json"
$latest = @{}
if (Test-Path $latestPath) {
    $obj = Get-Content $latestPath -Raw | ConvertFrom-Json
    foreach ($p in $obj.PSObject.Properties) { $latest[$p.Name] = $p.Value }
}
$latest[$appId] = [ordered]@{
    versionCode = $code
    versionName = $name
    file        = $file
    notes       = $Notes
    publishedAt = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
}
# UTF-8 without a BOM (Set-Content -Encoding utf8 adds one on PowerShell 5.1)
[System.IO.File]::WriteAllText($latestPath, ($latest | ConvertTo-Json -Depth 4), (New-Object System.Text.UTF8Encoding $false))
Write-Host "Published $appId $name ($code) -> $dir\$file"
