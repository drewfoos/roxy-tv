# Plays test-symbols-fallback (ASS with symbols/emoji/CJK and no attached fonts) on the
# connected device with the stats overlay hidden, screenshots each 6 s phase, and prints
# the libass font-selection log. Every "Glyph ... not found" should resolve to a bundled
# font, never to a /system colour emoji font.
param(
    [string]$Server = "http://192.168.1.71:8096",
    [string]$OutDir = "$PSScriptRoot\..\spike-runs\symbols-$(Get-Date -Format yyyyMMdd-HHmmss)",
    [string]$Adb = "$env:LOCALAPPDATA\Android\Sdk\platform-tools\adb.exe"
)
$ErrorActionPreference = "Continue"
$pkg = "tv.roxy.tv.debug"
New-Item -ItemType Directory -Force $OutDir | Out-Null
& $Adb shell am force-stop $pkg
& $Adb logcat -c
& $Adb shell am start -n "$pkg/tv.roxy.tv.player.PlayerActivity" --es url "$Server/stream/test-symbols-fallback" --es title symbols --es hwdec mediacodec --es vo gpu --es submode full --ez stats true | Out-Null
Start-Sleep 5
& $Adb shell input keyevent KEYCODE_DPAD_UP   # hide the overlay so the subtitles are readable
Start-Sleep 1
foreach ($i in 1..4) {
    & $Adb shell screencap -p /sdcard/sym.png
    & $Adb pull /sdcard/sym.png "$OutDir\screen$i.png" | Out-Null
    Start-Sleep 6
}
$p = (& $Adb shell pidof $pkg).Trim()
& $Adb logcat -d --pid=$p -v brief | Where-Object { $_ -match '^V/mpv|^E/mpv|^[IWE]/MpvPlayer' } | Set-Content -Encoding utf8 "$OutDir\mpv.log"
& $Adb shell am force-stop $pkg
"--- fonts.conf on device (first lines):"
& $Adb shell "run-as $pkg head -c 400 files/mpv/fonts.conf"
"`n--- font selection:"
Select-String -Path "$OutDir\mpv.log" -Pattern 'fontselect|Glyph|No usable|fontconfig' | ForEach-Object { $_.Line -replace '^.*MpvPlayer[^:]*: ', '' } | Where-Object { $_ -notmatch '^V/mpv' } | Select-Object -Unique
"screens: $OutDir"
