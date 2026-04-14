param(
    [string]$OutputDir = ".\dist",
    [string]$ModuleCacheDir = ".\.gomodcache"
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Add-Type -AssemblyName System.IO.Compression.FileSystem

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

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

Write-Host "Building standalone CLI..."
& $goExe build -o (Join-Path $OutputDir "gtf-cli.exe") .\cmd\gtf-cli
if ($LASTEXITCODE -ne 0) {
    throw "CLI build failed with exit code $LASTEXITCODE"
}

Write-Host "Building desktop app..."
if ($null -eq $gccCommand) {
    throw "gcc.exe not found. Install WinLibs/MinGW and add it to PATH before building the desktop UI."
}
$env:CGO_ENABLED = "1"
& $goExe build -ldflags "-H windowsgui" -o (Join-Path $OutputDir "GoogleTakeoutFixer.exe") .\cmd
if ($LASTEXITCODE -ne 0) {
    throw "Desktop app build failed with exit code $LASTEXITCODE"
}

$bundledMotionPhotoExe = Join-Path $repoRoot "motionphoto2.exe"
$bundledMotionPhotoZip = Join-Path $repoRoot "MotionPhoto2_Windows_v2.7.7.zip"
$outputMotionPhotoExe = Join-Path $OutputDir "motionphoto2.exe"

if (Test-Path $bundledMotionPhotoExe) {
    try {
        Copy-Item -LiteralPath $bundledMotionPhotoExe -Destination $outputMotionPhotoExe -Force
        Write-Host "Bundled motionphoto2.exe into $OutputDir"
    } catch {
        if (Test-Path $outputMotionPhotoExe) {
            Write-Host "Keeping existing motionphoto2.exe in $OutputDir"
        } else {
            throw
        }
    }
} elseif (Test-Path $bundledMotionPhotoZip) {
    if (Test-Path $outputMotionPhotoExe) {
        Write-Host "Keeping existing motionphoto2.exe in $OutputDir"
    } else {
    $zip = [System.IO.Compression.ZipFile]::OpenRead($bundledMotionPhotoZip)
    try {
        $entry = $zip.Entries | Where-Object { $_.FullName -ieq "motionphoto2.exe" } | Select-Object -First 1
        if ($null -eq $entry) {
            throw "motionphoto2.exe not found inside $bundledMotionPhotoZip"
        }
        [System.IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $outputMotionPhotoExe, $true)
        Write-Host "Extracted motionphoto2.exe into $OutputDir"
    } finally {
        $zip.Dispose()
    }
    }
}

Write-Host "Build completed in $OutputDir"
Write-Host "Using module cache $resolvedModuleCache"
