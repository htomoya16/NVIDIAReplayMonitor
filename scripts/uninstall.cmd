@echo off
set "NRM_UNINSTALL_SCRIPT=%~f0"
rem End batch interpretation before the loaded PowerShell body deletes this file.
(goto) 2>nul & powershell.exe -NoLogo -NoProfile -Command "$source=[IO.File]::ReadAllText($env:NRM_UNINSTALL_SCRIPT); $marker='# NRM_POWERSHELL_BODY'; $result=& ([ScriptBlock]::Create($source.Substring($source.LastIndexOf($marker)+$marker.Length))); exit ([int]$result)"
# NRM_POWERSHELL_BODY
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$root = [IO.Path]::GetDirectoryName([IO.Path]::GetFullPath($env:NRM_UNINSTALL_SCRIPT))
$exitCode = 1
$stage = 'restore'
$details = ''
$mutex = $null
$ownsMutex = $false
$reportPath = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'NVIDIAReplayMonitor\uninstall-error.txt'

function InstallFile([string]$name) {
    $path = [IO.Path]::GetFullPath([IO.Path]::Combine($root, $name))
    if (-not [string]::Equals([IO.Path]::GetDirectoryName($path), $root, [StringComparison]::OrdinalIgnoreCase)) { throw 'Unexpected cleanup path.' }
    if (Test-Path -LiteralPath $path) {
        $item = Get-Item -LiteralPath $path -Force
        if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Not a regular distribution file: $name" }
    }
    return $path
}

try {
    $sid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $mutex = [Threading.Mutex]::new($false, "Local\NVIDIAReplayMonitor.Uninstaller.$sid")
    try { $ownsMutex = $mutex.WaitOne(0) } catch [Threading.AbandonedMutexException] { $ownsMutex = $true }
    if (-not $ownsMutex) { throw 'Another uninstaller is running. Wait for it to finish.' }
    $exe = InstallFile 'NVIDIAReplayMonitor.exe'
    $self = InstallFile 'uninstall.cmd'
    $readme = InstallFile 'README.md'
    if (-not (Test-Path -LiteralPath $exe -PathType Leaf)) { throw 'NVIDIAReplayMonitor.exe was not found. Restore files have been kept.' }
    if (-not [string]::Equals($self, [IO.Path]::GetFullPath($env:NRM_UNINSTALL_SCRIPT), [StringComparison]::OrdinalIgnoreCase)) { throw 'Unexpected uninstaller filename.' }

    Write-Host 'Stopping the monitor and restoring settings. Initial setup may take time to finish.'
    $start = [Diagnostics.ProcessStartInfo]::new()
    $start.FileName = $exe
    $start.Arguments = '--uninstall'
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $start.StandardOutputEncoding = [Text.Encoding]::UTF8
    $start.StandardErrorEncoding = [Text.Encoding]::UTF8
    $process = [Diagnostics.Process]::Start($start)
    try {
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        # Wait for this exe, not the persistent NVIDIA process it restarts.
        if (-not $process.WaitForExit(240000)) { throw 'Uninstall did not finish within four minutes. It was not terminated; all files have been kept.' }
        $details = $stdout.GetAwaiter().GetResult() + $stderr.GetAwaiter().GetResult()
        if ($details) { Write-Host $details }
        if ($process.ExitCode -ne 0) { throw "Settings could not be restored (exit $($process.ExitCode)). No distribution files were deleted." }
    } finally { $process.Dispose() }

    $stage = 'file cleanup'
    $deleteReadme = $false
    if (Test-Path -LiteralPath $readme -PathType Leaf) {
        $sha = [Security.Cryptography.SHA256]::Create()
        $stream = [IO.File]::OpenRead($readme)
        try { $hash = [BitConverter]::ToString($sha.ComputeHash($stream)).Replace('-', '') }
        finally { $stream.Dispose(); $sha.Dispose() }
        $deleteReadme = $hash -eq '__README_SHA256__'
    }
    # Exact filenames only. Never remove a directory or use a wildcard.
    Remove-Item -LiteralPath $exe -ErrorAction Stop
    if ($deleteReadme) { Remove-Item -LiteralPath $readme -ErrorAction Stop }
    elseif (Test-Path -LiteralPath $readme) { Write-Host 'README.md was modified and has been kept.' }
    Remove-Item -LiteralPath $self -ErrorAction Stop
    if (Test-Path -LiteralPath $reportPath -PathType Leaf) { Remove-Item -LiteralPath $reportPath }
    Write-Host 'Uninstall completed. The exe and uninstaller were removed. Other files and the folder were kept.'
    $exitCode = 0
} catch {
    $message = "Stage: $stage`r`n$($_.Exception.Message)`r`n$details"
    Write-Host $message
    if ($ownsMutex) {
        try {
            [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($reportPath)) | Out-Null
            [IO.File]::WriteAllText($reportPath, ([DateTime]::Now.ToString('o') + "`r`n" + $message), [Text.UTF8Encoding]::new($false))
            Write-Host "Failure report: $reportPath"
        } catch { Write-Host 'The failure report could not be saved.' }
    }
    if ($stage -eq 'file cleanup') { Write-Host 'Settings restoration completed. Delete any remaining distribution files manually.' }
} finally {
    if ($ownsMutex) { $mutex.ReleaseMutex() }
    if ($mutex) { $mutex.Dispose() }
}
try { Read-Host 'Press Enter to close' | Out-Null } catch {}
return $exitCode
