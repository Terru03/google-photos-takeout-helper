param(
    [Parameter(Mandatory)][string]$Archive,
    [Parameter(Mandatory)][string]$Version
)

$ErrorActionPreference = 'Stop'
$Archive = (Resolve-Path -LiteralPath $Archive).Path
$tempRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$scratch = Join-Path $tempRoot ("takeout-smoke-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $scratch | Out-Null
$originalPath = $env:PATH
$originalTimezone = $env:TZ
$originalLocale = $env:LC_ALL

try {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    [System.IO.Compression.ZipFile]::ExtractToDirectory($Archive, $scratch)
    $bundle = Join-Path $scratch 'GoogleTakeoutFixer_Windows_x64'
    $app = Join-Path $bundle 'GoogleTakeoutFixer.exe'
    $exifTool = Join-Path $bundle 'exiftool.exe'
    foreach ($file in @($app, $exifTool, (Join-Path $bundle 'LICENSE.txt'), (Join-Path $bundle 'NOTICE.md'),
                        (Join-Path $bundle 'exiftool_files\LICENSE'), (Join-Path $bundle 'exiftool_files\Licenses_Strawberry_Perl.zip'))) {
        if (!(Test-Path -LiteralPath $file -PathType Leaf)) {
            throw "Release file missing: $file"
        }
    }

    $inputDir = Join-Path $scratch 'Sample Takeout'
    $outputDir = Join-Path $scratch 'Repaired Photos'
    New-Item -ItemType Directory -Path $inputDir | Out-Null
    $photo = Join-Path $inputDir 'sample.jpg'
    Add-Type -AssemblyName System.Drawing
    $image = [System.Drawing.Bitmap]::new(8, 8)
    try {
        $image.Save($photo, [System.Drawing.Imaging.ImageFormat]::Jpeg)
    } finally {
        $image.Dispose()
    }
    Set-Content -LiteralPath ($photo + '.json') -Encoding ascii -Value '{"title":"sample.jpg","photoTakenTime":{"timestamp":"1704067200"}}'
    $photoHash = (Get-FileHash -LiteralPath $photo).Hash

    # Run from outside the bundle without an installed ExifTool or compiler on PATH.
    $env:PATH = Join-Path $env:SystemRoot 'System32'
    $env:TZ = 'UTC'
    $env:LC_ALL = 'C'
    Push-Location -LiteralPath $scratch
    try {
        $appVersion = & $app -version
        if ($LASTEXITCODE -ne 0 -or $appVersion -cne $Version) {
            throw "Expected app version $Version, got $appVersion"
        }
        & $app -input $inputDir -output $outputDir -album-mode all -flatten -metadata-mode file -conflict-policy prefer-json -verify
        if ($LASTEXITCODE -ne 0) { throw 'Packaged app failed to process the sample.' }
        $repaired = Join-Path $outputDir 'sample.jpg'
        $date = & $exifTool -s3 -DateTimeOriginal $repaired
        if ($LASTEXITCODE -ne 0 -or $date -cne '2024:01:01 00:00:00') {
            throw "Expected capture timestamp on the repaired JPEG, got $date"
        }
        if ((Get-FileHash -LiteralPath $photo).Hash -cne $photoHash) {
            throw 'The original sample JPEG was modified.'
        }
    } finally {
        Pop-Location
    }
} finally {
    $env:PATH = $originalPath
    $env:TZ = $originalTimezone
    $env:LC_ALL = $originalLocale
    if ([System.IO.Path]::GetDirectoryName((Resolve-Path -LiteralPath $scratch).Path) -eq $tempRoot.TrimEnd('\')) {
        Remove-Item -LiteralPath $scratch -Recurse -Force
    }
}

Write-Host 'Packaged metadata repair and original preservation passed.'
