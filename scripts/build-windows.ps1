param(
    [string]$OutputDir = ".\dist",
    [string]$ModuleCacheDir = ".\.gomodcache",
    [ValidatePattern('^(dev|v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?)$')]
    [string]$Version = "dev"
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$repoRootFull = [System.IO.Path]::GetFullPath($repoRoot)
$repoRootPrefix = $repoRootFull.TrimEnd(
    [System.IO.Path]::DirectorySeparatorChar,
    [System.IO.Path]::AltDirectorySeparatorChar
) + [System.IO.Path]::DirectorySeparatorChar
$resolvedOutputDir = if ([System.IO.Path]::IsPathRooted($OutputDir)) {
    $OutputDir
} else {
    Join-Path $repoRoot $OutputDir
}
$OutputDir = [System.IO.Path]::GetFullPath($resolvedOutputDir)
if (
    $OutputDir -eq $repoRootFull -or
    $OutputDir.Length -lt 6 -or
    -not $OutputDir.StartsWith($repoRootPrefix, [System.StringComparison]::OrdinalIgnoreCase)
) {
    throw "Refuse to clean unsafe output directory: $OutputDir"
}

$goExe = "C:\Program Files\Go\bin\go.exe"
if (-not (Test-Path $goExe)) {
    $goCommand = Get-Command go -ErrorAction SilentlyContinue
    if ($null -eq $goCommand) {
        throw "Go not found. Install Go or add it to PATH."
    }
    $goExe = $goCommand.Source
}

$resolvedModuleCache = if ([System.IO.Path]::IsPathRooted($ModuleCacheDir)) {
    $ModuleCacheDir
} else {
    Join-Path $repoRoot $ModuleCacheDir
}
New-Item -ItemType Directory -Force -Path $resolvedModuleCache | Out-Null
$env:GOMODCACHE = $resolvedModuleCache

$preferredGccDir = "$env:LOCALAPPDATA\Microsoft\WinGet\Packages\BrechtSanders.WinLibs.MCF.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\mingw64\bin"
if (Test-Path (Join-Path $preferredGccDir "gcc.exe")) {
    $env:PATH = "$preferredGccDir;$env:PATH"
}

$gccCommand = Get-Command gcc -ErrorAction SilentlyContinue
if ($null -eq $gccCommand) {
    throw "gcc.exe not found. Install WinLibs/MinGW and add it to PATH before building GoogleTakeoutFixer.exe."
}
$env:CGO_ENABLED = "1"

if (Test-Path $OutputDir) {
    Remove-Item -LiteralPath $OutputDir -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

Write-Host "Building GoogleTakeoutFixer.exe..."
Push-Location -LiteralPath $repoRoot
try {
    $linkerFlags = "-X github.com/Terru03/google-photos-takeout-helper/internal/version.Tag=$Version"
    & $goExe build -trimpath -ldflags $linkerFlags -o (Join-Path $OutputDir "GoogleTakeoutFixer.exe") .\cmd
    if ($LASTEXITCODE -ne 0) {
        throw "Build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

Write-Host "Build completed in $OutputDir"
Write-Host "Using module cache $resolvedModuleCache"
