param(
    [string]$ModuleCacheDir = ".\.gomodcache",
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$AppArgs
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

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
    throw "gcc.exe not found. Install WinLibs/MinGW and add it to PATH before running the desktop UI."
}

$env:CGO_ENABLED = "1"

Write-Host "Running desktop app..."
Write-Host "Go: $goExe"
Write-Host "gcc: $($gccCommand.Source)"
Write-Host "GOMODCACHE: $resolvedModuleCache"

& $goExe run ./cmd @AppArgs
if ($LASTEXITCODE -ne 0) {
    throw "go run failed with exit code $LASTEXITCODE"
}
