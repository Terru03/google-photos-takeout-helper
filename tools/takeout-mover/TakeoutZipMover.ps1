Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Add-Type -AssemblyName System.IO.Compression.FileSystem

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ConfigPath = Join-Path $ScriptDir "takeout-mover-config.json"
$LogPath = Join-Path $ScriptDir "takeout-mover.log"
$ManifestPath = Join-Path $ScriptDir "takeout-mover-manifest.jsonl"

function Write-Log {
    param([string]$Message)
    $line = "[{0}] {1}" -f (Get-Date -Format "yyyy-MM-dd HH:mm:ss"), $Message
    Write-Host $line
    Add-Content -Path $LogPath -Value $line -Encoding UTF8
}

function Format-GB {
    param([Nullable[Int64]]$Bytes)
    if ($null -eq $Bytes -or $Bytes -le 0) { return "0 GB" }
    return ("{0:N1} GB" -f ($Bytes / 1GB))
}

function Get-DriveList {
    Get-CimInstance Win32_LogicalDisk |
        Where-Object { $_.DriveType -in 2,3 } |
        Sort-Object DeviceID |
        ForEach-Object {
            [PSCustomObject]@{
                Letter = $_.DeviceID
                Label = if ($_.VolumeName) { $_.VolumeName } else { "" }
                Total = [Int64]$_.Size
                Free  = [Int64]$_.FreeSpace
                Type  = if ($_.DriveType -eq 2) { "Removable" } else { "Fixed" }
                IsFinalBackup = ($_.DeviceID -ieq "B:" -or $_.VolumeName -ieq "Backup B")
                IsSystemDrive = ($_.DeviceID -ieq $env:SystemDrive)
            }
        }
}

function Show-Drives {
    Write-Host ""
    Write-Host "Detected drives:"
    Write-Host "----------------"
    Get-DriveList | ForEach-Object {
        $flags = @()
        if ($_.IsFinalBackup) { $flags += "FINAL_BACKUP_EXCLUDED" }
        if ($_.IsSystemDrive) { $flags += "SYSTEM" }
        $flagText = if ($flags.Count -gt 0) { " [" + ($flags -join ", ") + "]" } else { "" }

        Write-Host ("{0}  Label='{1}'  Type={2}  Free={3}  Total={4}{5}" -f `
            $_.Letter, $_.Label, $_.Type, (Format-GB $_.Free), (Format-GB $_.Total), $flagText)
    }
    Write-Host ""
}

function Ensure-Dir {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) {
        New-Item -ItemType Directory -Force -Path $Path | Out-Null
    }
}

function Read-Config {
    if (-not (Test-Path -LiteralPath $ConfigPath)) { return $null }
    return Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
}

function Save-Config {
    param($Config)
    $Config | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $ConfigPath -Encoding UTF8
}

function New-ConfigWizard {
    Show-Drives

    $incoming = Read-Host "Incoming/download folder [default: C:\Takeout_Incoming]"
    if ([string]::IsNullOrWhiteSpace($incoming)) {
        $incoming = "C:\Takeout_Incoming"
    }

    Write-Host ""
    Write-Host "Suggested ZIP storage drives, excluding B:, Backup B, and system drive:"
    $suggested = Get-DriveList |
        Where-Object { -not $_.IsFinalBackup -and -not $_.IsSystemDrive -and $_.Free -gt 20GB } |
        Sort-Object Free -Descending

    $suggested | ForEach-Object {
        Write-Host ("{0}\Takeout_Zips  Free={1}  Label='{2}'" -f $_.Letter, (Format-GB $_.Free), $_.Label)
    }

    Write-Host ""
    $useSuggested = Read-Host "Use suggested ZIP storage folders? Y/N [default: Y]"
    $targets = @()

    if ([string]::IsNullOrWhiteSpace($useSuggested) -or $useSuggested.Trim().ToUpperInvariant() -eq "Y") {
        $targets = $suggested | ForEach-Object { Join-Path ($_.Letter + "\") "Takeout_Zips" }
    } else {
        Write-Host "Enter target folders separated by semicolon."
        Write-Host "Example: D:\Takeout_Zips;F:\Takeout_Zips;G:\Takeout_Zips"
        $manual = Read-Host "Target folders"
        $targets = $manual.Split(";") | ForEach-Object { $_.Trim() } | Where-Object { $_ }
    }

    if ($targets.Count -eq 0) {
        throw "No target folders selected."
    }

    foreach ($target in $targets) {
        $root = [System.IO.Path]::GetPathRoot($target).TrimEnd("\")
        $drive = Get-DriveList | Where-Object { $_.Letter -ieq $root }
        if ($drive -and $drive.IsFinalBackup) {
            throw "Refusing to use final backup drive as ZIP storage: $target"
        }
    }

    $config = [PSCustomObject]@{
        IncomingFolder = $incoming
        TargetFolders = @($targets)
        SafetyFreeGB = 20
        StableSeconds = 120
        LoopSeconds = 30
        ExcludedDriveLabels = @("Backup B")
        ExcludedDriveLetters = @("B:")
    }

    Ensure-Dir $config.IncomingFolder
    $config.TargetFolders | ForEach-Object { Ensure-Dir $_ }

    Save-Config $config
    Write-Log "Created config: $ConfigPath"
    return $config
}

function Test-FileUnlocked {
    param([string]$Path)
    try {
        $stream = [System.IO.File]::Open($Path, "Open", "Read", "None")
        $stream.Close()
        return $true
    } catch {
        return $false
    }
}

function Test-ZipReadable {
    param([string]$Path)
    try {
        $zip = [System.IO.Compression.ZipFile]::OpenRead($Path)
        $null = $zip.Entries.Count
        $zip.Dispose()
        return $true
    } catch {
        return $false
    }
}

function Get-UniqueDestination {
    param(
        [string]$Folder,
        [string]$FileName
    )

    $dest = Join-Path $Folder $FileName
    if (-not (Test-Path -LiteralPath $dest)) { return $dest }

    $base = [System.IO.Path]::GetFileNameWithoutExtension($FileName)
    $ext = [System.IO.Path]::GetExtension($FileName)

    for ($i = 1; $i -lt 10000; $i++) {
        $candidate = Join-Path $Folder ("{0}_{1}{2}" -f $base, $i, $ext)
        if (-not (Test-Path -LiteralPath $candidate)) { return $candidate }
    }

    throw "Could not create unique destination for $FileName"
}

function Get-BestTarget {
    param(
        $Config,
        [Int64]$FileSize
    )

    $needed = $FileSize + ([Int64]$Config.SafetyFreeGB * 1GB)

    $validTargets = foreach ($folder in $Config.TargetFolders) {
        $root = [System.IO.Path]::GetPathRoot($folder).TrimEnd("\")
        $driveLetter = $root

        $drive = Get-DriveList | Where-Object { $_.Letter -ieq $driveLetter }
        if (-not $drive) { continue }
        if ($drive.IsFinalBackup) { continue }
        if ($drive.Free -lt $needed) { continue }

        [PSCustomObject]@{
            Folder = $folder
            Free = $drive.Free
        }
    }

    return $validTargets | Sort-Object Free -Descending | Select-Object -First 1
}

function Write-Manifest {
    param($Obj)
    $Obj | ConvertTo-Json -Compress -Depth 8 | Add-Content -LiteralPath $ManifestPath -Encoding UTF8
}

$config = Read-Config
if ($null -eq $config) {
    $config = New-ConfigWizard
}

Ensure-Dir $config.IncomingFolder
$config.TargetFolders | ForEach-Object { Ensure-Dir $_ }

Write-Log "Takeout ZIP mover started."
Write-Log "Incoming folder: $($config.IncomingFolder)"
Write-Log "Target folders: $($config.TargetFolders -join '; ')"
Write-Log "Final backup exclusion: B: and label Backup B"

$seen = @{}

while ($true) {
    try {
        $files = Get-ChildItem -LiteralPath $config.IncomingFolder -File -Filter "*.zip" -ErrorAction SilentlyContinue

        foreach ($file in $files) {
            $path = $file.FullName

            if ($path -match "\.(crdownload|part|tmp|fdmdownload)$") {
                continue
            }

            $file.Refresh()
            $now = Get-Date
            $currentSize = [Int64]$file.Length

            if (-not $seen.ContainsKey($path)) {
                $seen[$path] = [PSCustomObject]@{
                    Size = $currentSize
                    StableSince = $now
                }
                continue
            }

            if ($seen[$path].Size -ne $currentSize) {
                $seen[$path].Size = $currentSize
                $seen[$path].StableSince = $now
                continue
            }

            $stableFor = ($now - $seen[$path].StableSince).TotalSeconds
            if ($stableFor -lt [int]$config.StableSeconds) {
                continue
            }

            if (-not (Test-FileUnlocked $path)) {
                continue
            }

            if (-not (Test-ZipReadable $path)) {
                Write-Log "ZIP not readable yet, skipping for now: $($file.Name)"
                continue
            }

            $target = Get-BestTarget -Config $config -FileSize $currentSize
            if ($null -eq $target) {
                Write-Log "No target drive has enough free space for $($file.Name). Waiting."
                continue
            }

            Ensure-Dir $target.Folder
            $dest = Get-UniqueDestination -Folder $target.Folder -FileName $file.Name
            $moving = "$dest.moving"

            Write-Log "Copying $($file.Name) to $($target.Folder)"

            Copy-Item -LiteralPath $path -Destination $moving -Force

            $srcSize = (Get-Item -LiteralPath $path).Length
            $copySize = (Get-Item -LiteralPath $moving).Length

            if ($srcSize -ne $copySize) {
                Remove-Item -LiteralPath $moving -Force -ErrorAction SilentlyContinue
                throw "Size verification failed for $($file.Name): source=$srcSize copy=$copySize"
            }

            Rename-Item -LiteralPath $moving -NewName ([System.IO.Path]::GetFileName($dest))

            $finalSize = (Get-Item -LiteralPath $dest).Length
            if ($finalSize -ne $srcSize) {
                throw "Final verification failed for $($file.Name)"
            }

            Remove-Item -LiteralPath $path -Force

            Write-Log "Moved successfully: $($file.Name) -> $dest"

            Write-Manifest ([PSCustomObject]@{
                time = (Get-Date).ToString("o")
                status = "moved"
                source = $path
                destination = $dest
                bytes = $srcSize
            })

            $seen.Remove($path)
        }
    } catch {
        Write-Log "ERROR: $($_.Exception.Message)"
        Write-Manifest ([PSCustomObject]@{
            time = (Get-Date).ToString("o")
            status = "error"
            error = $_.Exception.Message
        })
    }

    Start-Sleep -Seconds ([int]$config.LoopSeconds)
}
