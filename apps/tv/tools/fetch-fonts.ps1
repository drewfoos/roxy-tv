# Downloads the fallback font bundle into app/src/main/assets/fonts/ (git-ignored) and
# writes FONTS.md with sizes, sha256 and licences. libass uses these only when an ASS
# track asks for a font the MKV does not attach.
#
# Why these fonts: Crunchyroll rips (the bulk of simulcast anime) style dialogue in
# "Open Sans Semibold" and never attach it; older rips and SRT-converted tracks ask for
# Arial; fansub groups commonly use Gandhi Sans, Lato, Montserrat, Source Sans, Fira,
# Cabin, Noto, and usually attach them but not always. DejaVu Sans is the glyph-coverage
# net (arrows, symbols, Greek/Cyrillic) and the Verdana stand-in. Liberation (Arimo
# metric-compatible) and Carlito stand in for Arial/Helvetica and Calibri via fontconfig
# aliases written by MpvPlayer. Noto Sans CJK JP covers Japanese signs.
param(
    [string]$Dest = "$PSScriptRoot\..\app\src\main\assets\fonts",
    [string]$NotoCJK = "$PSScriptRoot\..\..\..\testdata\fonts\NotoSansCJKjp-Regular.otf"
)
$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
New-Item -ItemType Directory -Force $Dest | Out-Null
$tmp = Join-Path $env:TEMP "roxy-fonts"; New-Item -ItemType Directory -Force $tmp | Out-Null

$direct = @(
    # family, file, url, licence
    @("Open Sans", "OpenSans-Regular.ttf",        "https://raw.githubusercontent.com/googlefonts/opensans/main/fonts/ttf/OpenSans-Regular.ttf", "OFL-1.1"),
    @("Open Sans", "OpenSans-Italic.ttf",         "https://raw.githubusercontent.com/googlefonts/opensans/main/fonts/ttf/OpenSans-Italic.ttf", "OFL-1.1"),
    @("Open Sans", "OpenSans-SemiBold.ttf",       "https://raw.githubusercontent.com/googlefonts/opensans/main/fonts/ttf/OpenSans-SemiBold.ttf", "OFL-1.1"),
    @("Open Sans", "OpenSans-SemiBoldItalic.ttf", "https://raw.githubusercontent.com/googlefonts/opensans/main/fonts/ttf/OpenSans-SemiBoldItalic.ttf", "OFL-1.1"),
    @("Open Sans", "OpenSans-Bold.ttf",           "https://raw.githubusercontent.com/googlefonts/opensans/main/fonts/ttf/OpenSans-Bold.ttf", "OFL-1.1"),
    @("Open Sans", "OpenSans-BoldItalic.ttf",     "https://raw.githubusercontent.com/googlefonts/opensans/main/fonts/ttf/OpenSans-BoldItalic.ttf", "OFL-1.1"),
    @("Lato", "Lato-Regular.ttf",    "https://raw.githubusercontent.com/google/fonts/main/ofl/lato/Lato-Regular.ttf", "OFL-1.1"),
    @("Lato", "Lato-Italic.ttf",     "https://raw.githubusercontent.com/google/fonts/main/ofl/lato/Lato-Italic.ttf", "OFL-1.1"),
    @("Lato", "Lato-Bold.ttf",       "https://raw.githubusercontent.com/google/fonts/main/ofl/lato/Lato-Bold.ttf", "OFL-1.1"),
    @("Lato", "Lato-BoldItalic.ttf", "https://raw.githubusercontent.com/google/fonts/main/ofl/lato/Lato-BoldItalic.ttf", "OFL-1.1"),
    @("Montserrat", "Montserrat-Regular.ttf",  "https://raw.githubusercontent.com/JulietaUla/Montserrat/master/fonts/ttf/Montserrat-Regular.ttf", "OFL-1.1"),
    @("Montserrat", "Montserrat-Italic.ttf",   "https://raw.githubusercontent.com/JulietaUla/Montserrat/master/fonts/ttf/Montserrat-Italic.ttf", "OFL-1.1"),
    @("Montserrat", "Montserrat-SemiBold.ttf", "https://raw.githubusercontent.com/JulietaUla/Montserrat/master/fonts/ttf/Montserrat-SemiBold.ttf", "OFL-1.1"),
    @("Montserrat", "Montserrat-Bold.ttf",     "https://raw.githubusercontent.com/JulietaUla/Montserrat/master/fonts/ttf/Montserrat-Bold.ttf", "OFL-1.1"),
    @("Source Sans 3", "SourceSans3-Regular.ttf", "https://raw.githubusercontent.com/adobe-fonts/source-sans/release/TTF/SourceSans3-Regular.ttf", "OFL-1.1"),
    @("Source Sans 3", "SourceSans3-It.ttf",      "https://raw.githubusercontent.com/adobe-fonts/source-sans/release/TTF/SourceSans3-It.ttf", "OFL-1.1"),
    @("Source Sans 3", "SourceSans3-Semibold.ttf","https://raw.githubusercontent.com/adobe-fonts/source-sans/release/TTF/SourceSans3-Semibold.ttf", "OFL-1.1"),
    @("Source Sans 3", "SourceSans3-Bold.ttf",    "https://raw.githubusercontent.com/adobe-fonts/source-sans/release/TTF/SourceSans3-Bold.ttf", "OFL-1.1"),
    @("Fira Sans", "FiraSans-Regular.ttf", "https://raw.githubusercontent.com/mozilla/Fira/master/ttf/FiraSans-Regular.ttf", "OFL-1.1"),
    @("Fira Sans", "FiraSans-Italic.ttf",  "https://raw.githubusercontent.com/mozilla/Fira/master/ttf/FiraSans-Italic.ttf", "OFL-1.1"),
    @("Fira Sans", "FiraSans-Bold.ttf",    "https://raw.githubusercontent.com/mozilla/Fira/master/ttf/FiraSans-Bold.ttf", "OFL-1.1"),
    @("Cabin", "Cabin-Regular.ttf", "https://raw.githubusercontent.com/impallari/Cabin/master/fonts/TTF/Cabin-Regular.ttf", "OFL-1.1"),
    @("Cabin", "Cabin-Bold.ttf",    "https://raw.githubusercontent.com/impallari/Cabin/master/fonts/TTF/Cabin-Bold.ttf", "OFL-1.1"),
    @("Noto Sans", "NotoSans-Regular.ttf", "https://raw.githubusercontent.com/notofonts/notofonts.github.io/main/fonts/NotoSans/hinted/ttf/NotoSans-Regular.ttf", "OFL-1.1"),
    @("Noto Sans", "NotoSans-Italic.ttf",  "https://raw.githubusercontent.com/notofonts/notofonts.github.io/main/fonts/NotoSans/hinted/ttf/NotoSans-Italic.ttf", "OFL-1.1"),
    @("Noto Sans", "NotoSans-Bold.ttf",    "https://raw.githubusercontent.com/notofonts/notofonts.github.io/main/fonts/NotoSans/hinted/ttf/NotoSans-Bold.ttf", "OFL-1.1"),
    @("Carlito", "Carlito-Regular.ttf", "https://raw.githubusercontent.com/googlefonts/carlito/main/fonts/ttf/Carlito-Regular.ttf", "OFL-1.1"),
    @("Carlito", "Carlito-Bold.ttf",    "https://raw.githubusercontent.com/googlefonts/carlito/main/fonts/ttf/Carlito-Bold.ttf", "OFL-1.1"),
    @("Oswald", "Oswald-Regular.ttf", "https://raw.githubusercontent.com/google/fonts/main/ofl/oswald/Oswald%5Bwght%5D.ttf", "OFL-1.1"),
    # Symbols and monochrome emoji: fansubs use ♀ ♂ ♪ ★ ☆ → ♥ and the odd emoji; libass
    # cannot rasterise colour bitmap emoji fonts (NotoColorEmoji), so provide outline ones.
    @("Noto Sans Symbols",  "NotoSansSymbols-Regular.ttf",  "https://raw.githubusercontent.com/notofonts/notofonts.github.io/main/fonts/NotoSansSymbols/hinted/ttf/NotoSansSymbols-Regular.ttf", "OFL-1.1"),
    @("Noto Sans Symbols 2","NotoSansSymbols2-Regular.ttf", "https://raw.githubusercontent.com/notofonts/notofonts.github.io/main/fonts/NotoSansSymbols2/hinted/ttf/NotoSansSymbols2-Regular.ttf", "OFL-1.1"),
    @("Noto Sans Math",     "NotoSansMath-Regular.ttf",     "https://raw.githubusercontent.com/notofonts/notofonts.github.io/main/fonts/NotoSansMath/hinted/ttf/NotoSansMath-Regular.ttf", "OFL-1.1"),
    @("Noto Music",         "NotoMusic-Regular.ttf",        "https://raw.githubusercontent.com/notofonts/notofonts.github.io/main/fonts/NotoMusic/hinted/ttf/NotoMusic-Regular.ttf", "OFL-1.1"),
    @("Noto Emoji",         "NotoEmoji-Regular.ttf",        "https://raw.githubusercontent.com/google/fonts/main/ofl/notoemoji/NotoEmoji%5Bwght%5D.ttf", "OFL-1.1")
)

$manifest = @()
function Add-Manifest([string]$family, [string]$file, [string]$lic, [string]$src) {
    $f = Get-Item (Join-Path $Dest $file)
    $h = (Get-FileHash $f.FullName -Algorithm SHA256).Hash.ToLower()
    $script:manifest += [pscustomobject]@{ family = $family; file = $file; kb = [math]::Round($f.Length / 1KB); sha256 = $h; licence = $lic; source = $src }
}

foreach ($d in $direct) {
    $family, $file, $url, $lic = $d
    $out = Join-Path $Dest $file
    if (-not (Test-Path $out)) {
        Write-Host "  $file" -NoNewline
        curl.exe -sSL -o $out $url
        if ($LASTEXITCODE -ne 0 -or (Get-Item $out).Length -lt 10KB) { Remove-Item $out -ErrorAction SilentlyContinue; throw "download failed: $url" }
        Write-Host "  ok"
    }
    Add-Manifest $family $file $lic $url
}

# Liberation Sans (Arial/Helvetica metric-compatible), from the tarball.
$libUrl = "https://github.com/liberationfonts/liberation-fonts/files/7261482/liberation-fonts-ttf-2.1.5.tar.gz"
if (-not (Test-Path (Join-Path $Dest "LiberationSans-Regular.ttf"))) {
    $tgz = Join-Path $tmp "liberation.tar.gz"; curl.exe -sSL -o $tgz $libUrl
    tar -xzf $tgz -C $tmp
    foreach ($fam in "LiberationSans", "LiberationSerif") {
        foreach ($s in "Regular", "Italic", "Bold", "BoldItalic") { Copy-Item (Get-ChildItem $tmp -Recurse -Filter "$fam-$s.ttf" | Select-Object -First 1).FullName $Dest -Force }
    }
}
foreach ($s in "Regular", "Italic", "Bold", "BoldItalic") { Add-Manifest "Liberation Sans" "LiberationSans-$s.ttf" "OFL-1.1" $libUrl }
foreach ($s in "Regular", "Italic", "Bold", "BoldItalic") { Add-Manifest "Liberation Serif" "LiberationSerif-$s.ttf" "OFL-1.1" $libUrl }

# DejaVu Sans: glyph-coverage net (arrows, symbols) and Verdana stand-in.
$djUrl = "https://github.com/dejavu-fonts/dejavu-fonts/releases/download/version_2_37/dejavu-fonts-ttf-2.37.zip"
if (-not (Test-Path (Join-Path $Dest "DejaVuSans.ttf"))) {
    $zip = Join-Path $tmp "dejavu.zip"; curl.exe -sSL -o $zip $djUrl
    Expand-Archive -Path $zip -DestinationPath (Join-Path $tmp "dejavu") -Force
    foreach ($f in "DejaVuSans.ttf", "DejaVuSans-Bold.ttf", "DejaVuSans-Oblique.ttf") { Copy-Item (Get-ChildItem (Join-Path $tmp "dejavu") -Recurse -Filter $f | Select-Object -First 1).FullName $Dest -Force }
}
foreach ($f in "DejaVuSans.ttf", "DejaVuSans-Bold.ttf", "DejaVuSans-Oblique.ttf") { Add-Manifest "DejaVu Sans" $f "Bitstream Vera / DejaVu (free, redistributable)" $djUrl }

# Gandhi Sans (Commie, DameDesuYo and others). Librerías Gandhi licence: free to install,
# distribute and use; no modification, no resale. CTAN's "gandhi" package carries the
# official OTFs (Fontsquirrel blocks scripted downloads).
$gsUrl = "https://mirrors.ctan.org/fonts/gandhi.zip"
if (-not (Test-Path (Join-Path $Dest "GandhiSans-Regular.otf"))) {
    $zip = Join-Path $tmp "gandhi.zip"; curl.exe -sSL -A "Mozilla/5.0" -o $zip $gsUrl
    Expand-Archive -Path $zip -DestinationPath (Join-Path $tmp "gandhi") -Force
    Get-ChildItem (Join-Path $tmp "gandhi") -Recurse -Include *.otf | Where-Object { $_.Name -match '^GandhiSans' } | ForEach-Object { Copy-Item $_.FullName $Dest -Force }
}
Get-ChildItem $Dest -Filter "GandhiSans*" | ForEach-Object { Add-Manifest "Gandhi Sans" $_.Name "Librerías Gandhi licence (free, no modification/resale)" $gsUrl }

# Symbola 9.00 (George Douros, 2016): one file covering nearly every symbol, dingbat and
# monochrome emoji block; the libass maintainers' suggested fallback. Versions from 11
# on are personal-use only, so take the 9.00 copy shipped inside the CRAN "fontquiver"
# package, whose embedded licence string reads "Fonts in this site are free for any use."
$fqUrl = "https://cran.r-project.org/src/contrib/fontquiver_0.2.1.tar.gz"
if (-not (Test-Path (Join-Path $Dest "Symbola.ttf"))) {
    $tgz = Join-Path $tmp "fontquiver.tar.gz"; curl.exe -sSL -o $tgz $fqUrl
    tar -xzf $tgz -C $tmp
    Copy-Item (Join-Path $tmp "fontquiver\inst\fonts\symbola-fonts\Symbola.ttf") $Dest -Force
}
Add-Manifest "Symbola" "Symbola.ttf" "Free for any use (v9.00 licence string)" $fqUrl

# Noto Sans CJK JP from the test-media fonts (already downloaded there).
if (Test-Path $NotoCJK) {
    Copy-Item $NotoCJK (Join-Path $Dest "NotoSansCJKjp-Regular.otf") -Force
    Add-Manifest "Noto Sans CJK JP" "NotoSansCJKjp-Regular.otf" "OFL-1.1" "https://github.com/notofonts/noto-cjk"
} else { Write-Warning "Noto CJK not found at $NotoCJK" }

$total = ($manifest | Measure-Object kb -Sum).Sum
$md = "# Bundled fallback fonts`n`nGenerated by tools/fetch-fonts.ps1 on $(Get-Date -Format yyyy-MM-dd). Total $([math]::Round($total/1024,1)) MB. Files live in app/src/main/assets/fonts/ (git-ignored); rerun the script to recreate them.`n`n| Family | File | KB | SHA-256 | Licence | Source |`n|---|---|---|---|---|---|`n"
foreach ($m in $manifest) { $md += "| $($m.family) | $($m.file) | $($m.kb) | ``$($m.sha256.Substring(0,16))…`` | $($m.licence) | $($m.source) |`n" }
$md | Set-Content -Encoding utf8 (Join-Path $PSScriptRoot "..\FONTS.md")
"`n$($manifest.Count) fonts, $([math]::Round($total/1024,1)) MB -> $Dest"
