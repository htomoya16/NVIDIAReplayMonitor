param([Parameter(Mandatory=$true)][string]$CompilerPath)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$projectRoot = Split-Path -Parent $PSScriptRoot
$testId = [Guid]::NewGuid().ToString('N')
$testRoot = Join-Path $projectRoot ('.cache\installer-tests\' + $testId + '\日本語 & ! %')
$outputDir = Join-Path $testRoot 'packages'
[IO.Directory]::CreateDirectory($outputDir) | Out-Null
$regKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\NRM.Test.' + $testId + '_is1'
$startup = Join-Path $testRoot 'startup\NVIDIAReplayMonitor.lnk'
$applicationDir = Join-Path $testRoot 'application'
$fixtureSource = Join-Path $testRoot 'fixture.go'

# This executable has no NVIDIA/MCP, process-control, or real-user data access.
$fixture = @'
package main
import ("fmt"; "os"; "path/filepath")
var version string
func main() {
    action, output := "", ""
    for i:=1;i<len(os.Args);i++ {
        if os.Args[i]=="--stop" || os.Args[i]=="--uninstall" { action=os.Args[i] }
        if os.Args[i]=="--output" && i+1<len(os.Args) { i++; output=os.Args[i] }
    }
    code, message := 0, "fixture "+version
    if action=="--stop" && os.Getenv("NRM_TEST_STOP_FAIL")=="1" { code=7;message="stop failed" }
    if action=="--uninstall" {
        if os.Getenv("NRM_TEST_RESTORE_FAIL")=="1" { code=7;message="restore failed" } else {
            exe,_:=os.Executable(); dir:=filepath.Dir(exe)
            for _,name:=range []string{"warning-state.json","manual-control.json"} { _=os.Remove(filepath.Join(dir,name)) }
            _=os.WriteFile(filepath.Join(dir,"restore-ok.txt"),[]byte("restored"),0600)
        }
    }
    if output!="" { if err:=os.WriteFile(output,[]byte(message),0600);err!=nil{os.Exit(8)} } else { fmt.Println(message) }
    os.Exit(code)
}
'@
[IO.File]::WriteAllText($fixtureSource, $fixture, [Text.UTF8Encoding]::new($false))

function Invoke-TestProcess([string]$file, [string]$arguments) {
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = $file
    $info.Arguments = $arguments
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $process = [Diagnostics.Process]::Start($info)
    try {
        if (-not $process.WaitForExit(60000)) { throw "Timed out without forced termination: $file" }
        return $process.ExitCode
    } finally { $process.Dispose() }
}

function InstalledKeyExists {
    $base = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::CurrentUser, [Microsoft.Win32.RegistryView]::Registry64)
    try {
        $key = $base.OpenSubKey($regKey)
        if ($key) { $key.Dispose(); return $true }
        return $false
    } finally { $base.Dispose() }
}

function Assert([bool]$condition, [string]$message) {
    if (-not $condition) { throw $message }
}

function Install([string]$package, [string]$extra = '', [bool]$success = $true) {
    $log = Join-Path $testRoot ('setup-' + [Guid]::NewGuid().ToString('N') + '.log')
    $arguments = '/CURRENTUSER /VERYSILENT /SUPPRESSMSGBOXES /NORESTART /SP- /DIR="' + $applicationDir + '" /LOG="' + $log + '" ' + $extra
    $code = Invoke-TestProcess $package $arguments
    Assert (($code -eq 0) -eq $success) "Setup exit=$code, expected success=$success. Log: $log"
}

function Uninstall([bool]$success) {
    $uninstaller = Join-Path $applicationDir 'unins000.exe'
    $log = Join-Path $testRoot ('uninstall-' + [Guid]::NewGuid().ToString('N') + '.log')
    $code = Invoke-TestProcess $uninstaller ('/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /LOG="' + $log + '"')
    Assert (($code -eq 0) -eq $success) "Uninstall exit=$code, expected success=$success. Log: $log"
    if ($success) {
        $timer = [Diagnostics.Stopwatch]::StartNew()
        while ((Test-Path -LiteralPath $uninstaller) -and $timer.Elapsed.TotalSeconds -lt 15) { Start-Sleep -Milliseconds 100 }
    }
}

$originalStop = $env:NRM_TEST_STOP_FAIL
$originalRestore = $env:NRM_TEST_RESTORE_FAIL
try {
    $env:NRM_TEST_STOP_FAIL = ''
    $env:NRM_TEST_RESTORE_FAIL = ''
    $packages = @()
    $payloads = @()
    foreach ($version in @('0.3.0-test', '0.3.1-test')) {
        $payload = Join-Path $testRoot $version
        [IO.Directory]::CreateDirectory($payload) | Out-Null
        go build -ldflags "-H=windowsgui -X main.version=$version" -o (Join-Path $payload 'NVIDIAReplayMonitor.exe') $fixtureSource
        if ($LASTEXITCODE -ne 0) { throw 'Fixture compilation failed' }
        [IO.File]::WriteAllText((Join-Path $payload 'README.md'), "Fixture README $version", [Text.UTF8Encoding]::new($false))
        $hash = (Get-FileHash -LiteralPath (Join-Path $payload 'README.md') -Algorithm SHA256).Hash
        & $CompilerPath '/Qp' "/DAppVersion=$version" "/DPayloadDir=$payload" "/DReadmeSHA256=$hash" "/DTestRoot=$testRoot" "/DTestId=$testId" "/O$outputDir" (Join-Path $projectRoot 'installer\NVIDIAReplayMonitor.iss')
        if ($LASTEXITCODE -ne 0) { throw 'Test installer compilation failed' }
        $packages += Join-Path $outputDir "NVIDIAReplayMonitor-$version-Setup.exe"
        $payloads += $payload
    }

    # A same-named shortcut owned by something else must not be overwritten.
    [IO.Directory]::CreateDirectory((Split-Path -Parent $startup)) | Out-Null
    $shell = New-Object -ComObject WScript.Shell
    $link = $shell.CreateShortcut($startup)
    $link.TargetPath = $env:ComSpec
    $link.Save()
    Install $packages[0] '/TASKS="startup"' $false
    Assert (-not (Test-Path -LiteralPath (Join-Path $applicationDir 'NVIDIAReplayMonitor.exe'))) 'Conflicting shortcut was ignored'
    Assert ($shell.CreateShortcut($startup).TargetPath -eq $env:ComSpec) 'Unrelated shortcut was modified'
    Remove-Item -LiteralPath $startup

    Install $packages[0] '/TASKS="startup"'
    Assert (InstalledKeyExists) 'Application was not registered'
    Assert (Test-Path -LiteralPath $startup) 'Startup link was not created'
    Assert ($shell.CreateShortcut($startup).TargetPath -eq (Join-Path $applicationDir 'NVIDIAReplayMonitor.exe')) 'Startup target differs'
    [IO.File]::WriteAllText((Join-Path $applicationDir 'keep.txt'), 'User file', [Text.UTF8Encoding]::new($false))
    foreach ($name in @('warning-state.json','manual-control.json')) { [IO.File]::WriteAllText((Join-Path $applicationDir $name), 'state must survive update') }
    $oldHash = (Get-FileHash -LiteralPath (Join-Path $applicationDir 'NVIDIAReplayMonitor.exe')).Hash

    $env:NRM_TEST_STOP_FAIL = '1'
    Install $packages[1] '' $false
    Assert ((Get-FileHash -LiteralPath (Join-Path $applicationDir 'NVIDIAReplayMonitor.exe')).Hash -eq $oldHash) 'Failed stop changed the executable'
    $env:NRM_TEST_STOP_FAIL = ''
    Install $packages[1]
    Assert ((Get-FileHash -LiteralPath (Join-Path $applicationDir 'NVIDIAReplayMonitor.exe')).Hash -eq (Get-FileHash -LiteralPath (Join-Path $payloads[1] 'NVIDIAReplayMonitor.exe')).Hash) 'Upgrade did not replace the executable'
    Assert (Test-Path -LiteralPath $startup) 'Upgrade lost the startup preference'
    foreach ($name in @('warning-state.json','manual-control.json')) {
        Assert ([IO.File]::ReadAllText((Join-Path $applicationDir $name)) -eq 'state must survive update') 'Upgrade changed saved state'
    }

    $env:NRM_TEST_RESTORE_FAIL = '1'
    Uninstall $false
    foreach ($name in @('NVIDIAReplayMonitor.exe','unins000.exe','warning-state.json','manual-control.json','keep.txt')) {
        Assert (Test-Path -LiteralPath (Join-Path $applicationDir $name)) "Failed restore deleted $name"
    }
    Assert (InstalledKeyExists) 'Failed uninstall removed registration'
    Assert (Test-Path -LiteralPath $startup) 'Failed restore removed startup'
    $env:NRM_TEST_RESTORE_FAIL = ''
    [IO.File]::AppendAllText((Join-Path $applicationDir 'README.md'), 'User modification')
    Uninstall $true
    Assert (-not (Test-Path -LiteralPath (Join-Path $applicationDir 'NVIDIAReplayMonitor.exe'))) 'Executable remains'
    Assert (-not (InstalledKeyExists)) 'Uninstall registration remains'
    Assert (-not (Test-Path -LiteralPath $startup)) 'Startup link remains'
    foreach ($name in @('README.md','keep.txt','restore-ok.txt')) { Assert (Test-Path -LiteralPath (Join-Path $applicationDir $name)) "User file missing: $name" }

    # A disabled startup choice must survive a later upgrade without /TASKS.
    Install $packages[0] '/TASKS=""'
    Install $packages[1]
    Assert (-not (Test-Path -LiteralPath $startup)) 'Disabled startup preference was lost'
    Uninstall $true
    Assert (-not (Test-Path -LiteralPath (Join-Path $applicationDir 'README.md'))) 'Unmodified README remains'
    Write-Output 'Installer tests passed: shortcut conflict, first install, failed stop, upgrade/state retention, failed restore, retry, user files, startup off.'
} finally {
    $env:NRM_TEST_STOP_FAIL = ''
    $env:NRM_TEST_RESTORE_FAIL = ''
    if (InstalledKeyExists) {
        try { Uninstall $true } catch { Write-Warning "Isolated test installation remains: $applicationDir. $($_.Exception.Message)" }
    }
    $env:NRM_TEST_STOP_FAIL = $originalStop
    $env:NRM_TEST_RESTORE_FAIL = $originalRestore
}
