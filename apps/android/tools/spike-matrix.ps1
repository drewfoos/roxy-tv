# Runs the player spike across test files x hwdec x vo on the connected adb device,
# screenshots each run with the stats overlay visible, saves the mpv log, and writes a
# results.csv. Needs: Roxy Server reachable from the device, the Roxy TV debug APK installed,
# adb on PATH or in the default SDK location.
#
#   .\tools\spike-matrix.ps1                       # all test files, all hwdec modes, vo=gpu
#   .\tools\spike-matrix.ps1 -Vos gpu,gpu-next -Files test-hi10p-ass
#   .\tools\spike-matrix.ps1 -Adb "adb" -Server http://192.168.1.50:8096
param(
    [string[]]$Files = @(
        "test-hi10p-ass", "test-hevc10-ass", "test-hevc10-hdr10-4k-ass", "test-dualaudio-ass", "test-av1-10bit-ass",
        "test-h264-8bit-ass", "test-hevc8-60fps-51-ass", "test-bluray-pgs", "test-hevc10-hlg-ass", "test-h264-mp4-movtext"
    ),
    [string[]]$Hwdecs = @("mediacodec-copy", "mediacodec", "no"),
    [string[]]$Vos = @("gpu"),
    [int]$PlaySeconds = 14,
    [int]$StartSeconds = 0,          # skip the intro on real releases
    [string]$SubMode = "full",       # full | signs | none
    [string]$Server = "http://192.168.1.71:8096",
    [string]$OutDir = "$PSScriptRoot\..\spike-runs\$(Get-Date -Format yyyyMMdd-HHmmss)",
    [string]$Adb = "$env:LOCALAPPDATA\Android\Sdk\platform-tools\adb.exe"
)
$ErrorActionPreference = "Continue"
$pkg = "tv.roxy.tv.debug"
New-Item -ItemType Directory -Force $OutDir | Out-Null
$results = @()
$bullet = [string][char]0x25CF  # selected-track marker in mpv's track list

foreach ($file in $Files) {
    foreach ($vo in $Vos) {
        foreach ($hw in $Hwdecs) {
            $tag = "$file--$hw--$vo"
            & $Adb shell am force-stop $pkg
            & $Adb logcat -c
            & $Adb shell am start -n "$pkg/tv.roxy.tv.player.PlayerActivity" --es url "$Server/stream/$file" --es title $file --es hwdec $hw --es vo $vo --ei start $StartSeconds --es submode $SubMode --ez stats true | Out-Null
            Start-Sleep $PlaySeconds
            & $Adb shell screencap -p /sdcard/roxy-matrix.png
            & $Adb pull /sdcard/roxy-matrix.png "$OutDir\$tag.png" | Out-Null
            $p = (& $Adb shell pidof $pkg).Trim()
            $log = & $Adb logcat -d --pid=$p -v brief | Where-Object { $_ -match '^V/mpv|^E/mpv|^[IWE]/MpvPlayer|AndroidRuntime|FATAL' }
            $log | Set-Content -Encoding utf8 "$OutDir\$tag.log"

            $join = ($log -join "`n")
            $decode = if ($join -match 'Using hardware decoding \(([^)]+)\)') { "hw:" + $Matches[1] } elseif ($join -match 'Using software decoding') { "software" } else { "?" }
            $failed = ($join -match 'Could not open codec')
            $fonts = ([regex]::Matches($join, 'fontselect: \(([^)]+)\) -> ([^,]+)') | ForEach-Object { "$($_.Groups[1].Value) -> $($_.Groups[2].Value)" } | Select-Object -Unique) -join "; "
            $sub = if ($join -match ($bullet + ' Subs\s+--sid=(\d+)[^\n]*\((\w+)\)')) { "sid=$($Matches[1]) $($Matches[2])" } else { "none" }
            $crash = ($join -match 'FATAL|AndroidRuntime')
            $results += [pscustomobject]@{ file = $file; hwdec = $hw; vo = $vo; decode = $decode; hwdecFailed = $failed; sub = $sub; fonts = $fonts; crash = $crash }
            "$tag : decode=$decode hwdecFailed=$failed sub=$sub crash=$crash"
        }
    }
}
& $Adb shell am force-stop $pkg
$results | Export-Csv -NoTypeInformation -Encoding utf8 "$OutDir\results.csv"
"`nWrote $OutDir\results.csv (drops / CPU are on the overlay in each PNG)"
