$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$qa = Join-Path $projectRoot ('.cache\uninstall-tests\' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $qa -Force | Out-Null

# This fixture never accesses NVIDIA, startup registration or user settings.
$fixture = @'
package main
import ("fmt"; "os"; "strconv")
func main() { code,_:=strconv.Atoi(os.Getenv("NRM_TEST_RESULT")); fmt.Printf("Test result: %d\n",code); os.Exit(code) }
'@
$fixturePath = Join-Path $qa 'fixture.go'
[IO.File]::WriteAllText($fixturePath, $fixture, [Text.UTF8Encoding]::new($false))
$fixtureExe = Join-Path $qa 'fixture.exe'
go build -ldflags '-H=windowsgui' -o $fixtureExe $fixturePath
if ($LASTEXITCODE -ne 0) { throw 'Uninstall fixture build failed' }

$scriptText = [IO.File]::ReadAllText((Join-Path $projectRoot 'dist\uninstall.cmd'))
# Keep diagnostic reports and the singleton lock isolated from real users.
$scriptText = $scriptText.Replace("[Environment]::GetFolderPath('LocalApplicationData')", '$root')
$scriptText = $scriptText.Replace('NVIDIAReplayMonitor.Uninstaller.', ('NVIDIAReplayMonitor.QA.' + [Guid]::NewGuid().ToString('N') + '.'))
foreach ($case in @('failure', 'success', 'edited-readme', 'unicode-日本語 & ! %')) {
    $folder = Join-Path $qa $case
    New-Item -ItemType Directory -Path $folder | Out-Null
    Copy-Item -LiteralPath $fixtureExe -Destination (Join-Path $folder 'NVIDIAReplayMonitor.exe')
    [IO.File]::WriteAllText((Join-Path $folder 'uninstall.cmd'), $scriptText, [Text.ASCIIEncoding]::new())
    Copy-Item -LiteralPath (Join-Path $projectRoot 'dist\README.md') -Destination (Join-Path $folder 'README.md')
    Set-Content -LiteralPath (Join-Path $folder 'keep.txt') -Value 'User file must remain.'
    if ($case -eq 'edited-readme') { Add-Content -LiteralPath (Join-Path $folder 'README.md') -Value 'User edit' }

    $start = [Diagnostics.ProcessStartInfo]::new()
    $start.FileName = $env:ComSpec
    $start.Arguments = '/d /c ""' + (Join-Path $folder 'uninstall.cmd') + '""'
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardInput = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $start.EnvironmentVariables['NRM_TEST_RESULT'] = if ($case -eq 'failure') { '7' } else { '0' }
    $process = [Diagnostics.Process]::Start($start)
    try {
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        $process.StandardInput.WriteLine('')
        $process.StandardInput.Close()
        if (-not $process.WaitForExit(20000)) { throw "Uninstall test timed out: $case. Test process was not terminated." }
        $output = $stdout.GetAwaiter().GetResult() + $stderr.GetAwaiter().GetResult()
        $expectedExit = if ($case -eq 'failure') { 1 } else { 0 }
        if ($process.ExitCode -ne $expectedExit) { throw "Exit code differs in $case`: $output" }
        $remaining = @(Get-ChildItem -LiteralPath $folder -File | Select-Object -ExpandProperty Name)
        $expected = if ($case -eq 'failure') { @('NVIDIAReplayMonitor.exe', 'uninstall.cmd', 'README.md', 'keep.txt') } elseif ($case -eq 'edited-readme') { @('README.md', 'keep.txt') } else { @('keep.txt') }
        if (@(Compare-Object ($remaining | Sort-Object) ($expected | Sort-Object)).Count) { throw "Remaining files differ in $case`: $remaining`n$output" }
        Write-Output "Uninstall test passed: $case"
    } finally { $process.Dispose() }
}
