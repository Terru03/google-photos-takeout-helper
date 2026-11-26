param(
    [Parameter(Mandatory)]
    [ValidatePattern('^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$')]
    [string]$Version,
    [string]$ExifToolArchive
)

$ErrorActionPreference = 'Stop'
$exifToolVersion = '13.64'
# Published in https://downloads.sourceforge.net/project/exiftool/checksums-13.64.txt
$exifToolSHA256 = '72C29C44D1E860EEBCAEFA71692D979AF9858FE9A6D7D791A5E7F1DAC77804AF'
$exifToolName = "exiftool-${exifToolVersion}_64"
$assetName = 'GoogleTakeoutFixer_Windows_x64'
$repoRoot = Split-Path -Parent $PSScriptRoot
$outputRoot = Join-Path $repoRoot 'dist'
$stage = Join-Path $outputRoot ("release-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $stage -Force | Out-Null

try {
    $stagedExifToolArchive = Join-Path $stage "$exifToolName.zip"
    if ($ExifToolArchive) {
        Copy-Item -LiteralPath $ExifToolArchive -Destination $stagedExifToolArchive
    } else {
        $downloadUrl = "https://downloads.sourceforge.net/project/exiftool/$exifToolName.zip"
        & curl.exe --fail --silent --show-error --location --max-time 120 --proto '=https' --proto-redir '=https' --output $stagedExifToolArchive $downloadUrl
        if ($LASTEXITCODE -ne 0) { throw 'Could not download the official ExifTool archive.' }
    }
    $actualHash = (Get-FileHash -LiteralPath $stagedExifToolArchive -Algorithm SHA256).Hash
    if ($actualHash -cne $exifToolSHA256) {
        throw "ExifTool archive SHA-256 mismatch. Expected $exifToolSHA256, got $actualHash."
    }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    [System.IO.Compression.ZipFile]::ExtractToDirectory($stagedExifToolArchive, $stage)
    $exifToolRoot = Join-Path $stage $exifToolName
    $bundle = Join-Path $stage $assetName
    $previousGOARCH = $env:GOARCH
    try {
        $env:GOARCH = 'amd64'
        & (Join-Path $PSScriptRoot 'build-windows.ps1') -OutputDir $bundle -Version $Version
    } finally {
        $env:GOARCH = $previousGOARCH
    }

    Copy-Item -LiteralPath (Join-Path $exifToolRoot 'exiftool(-k).exe') -Destination (Join-Path $bundle 'exiftool.exe')
    Copy-Item -LiteralPath (Join-Path $exifToolRoot 'exiftool_files') -Destination $bundle -Recurse
    Copy-Item -LiteralPath (Join-Path $exifToolRoot 'README.txt') -Destination (Join-Path $bundle 'ExifTool-README.txt')
    Copy-Item -LiteralPath (Join-Path $repoRoot 'LICENSE') -Destination (Join-Path $bundle 'LICENSE.txt')
    Copy-Item -LiteralPath (Join-Path $repoRoot 'README.md'), (Join-Path $repoRoot 'NOTICE.md') -Destination $bundle

    $stagedArchive = Join-Path $stage "$assetName.zip"
    [System.IO.Compression.ZipFile]::CreateFromDirectory($bundle, $stagedArchive, [System.IO.Compression.CompressionLevel]::Optimal, $true)
    & (Join-Path $PSScriptRoot 'test-windows-release.ps1') -Archive $stagedArchive -Version $Version
    Move-Item -LiteralPath $stagedArchive -Destination (Join-Path $outputRoot "$assetName.zip") -Force
    Write-Host "Release archive: $outputRoot\$assetName.zip"
} finally {
    if ([System.IO.Path]::GetDirectoryName((Resolve-Path -LiteralPath $stage).Path) -eq $outputRoot) {
        Remove-Item -LiteralPath $stage -Recurse -Force
    }
}
