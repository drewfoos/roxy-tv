# Plays one file on the connected adb device and exercises seeking: three +10 s seeks,
# then a long jump via repeated seeks, then two -10 s seeks, screenshotting the stats
# overlay after each phase. Shows whether range requests, cache refill and A/V resync
# behave on a large real file.
#
#   .\tools\spike-seek.ps1 -File "<source id or file-name stem>" -Hwdec mediacodec
param(
    [Parameter(Mandatory = $true)][string]$File,
    [string]$Hwdec = "mediacodec",
    [string]$Vo = "gpu",
    [int]$StartSeconds = 0,          # skip the intro (real releases: ~3-5 min of OP/logos)
    [string]$SubMode = "full",       # full | signs | none
    [int]$Sid = 0,                   # force an mpv subtitle id (bypasses the policy)
    [string]$Server = "http://192.168.1.71:8096",
    [string]$OutDir = "$PSScriptRoot\..\spike-runs\seek-$(Get-Date -Format yyyyMMdd-HHmmss)",
    [string]$Adb = "$env:LOCALAPPDATA\Android\Sdk\platform-tools\adb.exe"
)
$ErrorActionPreference = "Continue"
$pkg = "tv.roxy.tv.debug"
New-Item -ItemType Directory -Force $OutDir | Out-Null

function Snap([string]$name) {
    & $Adb shell screencap -p /sdcard/roxy-seek.png
    & $Adb pull /sdcard/roxy-seek.png "$OutDir\$name.png" | Out-Null
    "snap: $name"
}

& $Adb shell am force-stop $pkg
& $Adb logcat -c
# The URL is encoded for a stem; a source id works as-is.
$url = "$Server/stream/" + [uri]::EscapeDataString($File)
& $Adb shell am start -n "$pkg/tv.roxy.tv.player.PlayerActivity" --es url "$url" --es title "$File" --es hwdec $Hwdec --es vo $Vo --ei start $StartSeconds --es submode $SubMode --ei sid $Sid --ez stats true | Out-Null
Start-Sleep 12
Snap "1-start"

# +10 s x3, spaced so each seek completes
1..3 | ForEach-Object { & $Adb shell input keyevent KEYCODE_DPAD_RIGHT; Start-Sleep 3 }
Start-Sleep 4
Snap "2-after-3x-forward"

# long jump: 30 more forward seeks (~5 min in) fired quickly, then settle
1..30 | ForEach-Object { & $Adb shell input keyevent KEYCODE_DPAD_RIGHT; Start-Sleep -Milliseconds 250 }
Start-Sleep 8
Snap "3-after-long-jump"

# back twice
1..2 | ForEach-Object { & $Adb shell input keyevent KEYCODE_DPAD_LEFT; Start-Sleep 3 }
Start-Sleep 4
Snap "4-after-2x-back"

$p = (& $Adb shell pidof $pkg).Trim()
& $Adb logcat -d --pid=$p -v brief | Where-Object { $_ -match '^V/mpv|^E/mpv|^[IWE]/MpvPlayer|AndroidRuntime|FATAL' } | Set-Content -Encoding utf8 "$OutDir\mpv.log"
& $Adb shell am force-stop $pkg
"log: $OutDir\mpv.log"
