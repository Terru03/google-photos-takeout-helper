param(
    [string]$OutputDir = ".\dist",
    [string]$ModuleCacheDir = ".\.gomodcache"
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$repoRootFull = [System.IO.Path]::GetFullPath($repoRoot)
$resolvedOutputDir = if ([System.IO.Path]::IsPathRooted($OutputDir)) {
    $OutputDir
} else {
    Join-Path $repoRoot $OutputDir
}
$OutputDir = [System.IO.Path]::GetFullPath($resolvedOutputDir)
if ($OutputDir -eq $repoRootFull -or $OutputDir.Length -lt 6) {
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
& $goExe build -o (Join-Path $OutputDir "GoogleTakeoutFixer.exe") .\cmd
if ($LASTEXITCODE -ne 0) {
    throw "Build failed with exit code $LASTEXITCODE"
}

Write-Host "Build completed in $OutputDir"
Write-Host "Using module cache $resolvedModuleCache"
