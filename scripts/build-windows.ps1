param(
    [string]$OutputDir = ".\dist"
)

$ErrorActionPreference = "Stop"

$goExe = "C:\Program Files\Go\bin\go.exe"
if (-not (Test-Path $goExe)) {
    $goExe = "go"
}

$gccPath = "$env:LOCALAPPDATA\Microsoft\WinGet\Packages\BrechtSanders.WinLibs.MCF.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\mingw64\bin"
if (Test-Path $gccPath) {
    $env:PATH = "$gccPath;$env:PATH"
}

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

Write-Host "Building standalone CLI..."
& $goExe build -o (Join-Path $OutputDir "gtf-cli.exe") .\cmd\gtf-cli

Write-Host "Building desktop app..."
$env:CGO_ENABLED = "1"
& $goExe build -o (Join-Path $OutputDir "GoogleTakeoutFixer.exe") .\cmd

Write-Host "Build completed in $OutputDir"
