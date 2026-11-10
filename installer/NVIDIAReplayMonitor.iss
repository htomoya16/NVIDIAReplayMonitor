#ifndef AppVersion
  #define AppVersion "dev"
#endif
#ifndef NumericVersion
  #define NumericVersion "0.0.0.0"
#endif
#ifndef PayloadDir
  #error PayloadDir is required
#endif
#ifndef ReadmeSHA256
  #error ReadmeSHA256 is required
#endif

#ifdef TestRoot
  #define ProductId "NRM.Test." + TestId
  #define StartupDir TestRoot + "\startup"
  #define MenuDir TestRoot + "\menu"
  #define GateName "Local\NVIDIAReplayMonitor.Installer.Test." + TestId
#else
  #define ProductId "htomoya16.NVIDIAReplayMonitor"
  #define StartupDir "{userstartup}"
  #define MenuDir "{userprograms}\NVIDIAReplayMonitor"
  #define GateName "Local\NVIDIAReplayMonitor.Installer"
#endif

[Setup]
AppId={#ProductId}
AppName=NVIDIAReplayMonitor
AppVersion={#AppVersion}
AppPublisher=htomoya16
AppSupportURL=https://github.com/htomoya16/NVIDIAReplayMonitor
AppUpdatesURL=https://github.com/htomoya16/NVIDIAReplayMonitor/releases
DefaultDirName={localappdata}\NVIDIAReplayMonitor
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.22000
UsePreviousAppDir=yes
UsePreviousTasks=yes
DisableDirPage=auto
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\NVIDIAReplayMonitor.exe
UninstallFilesDir={app}
CloseApplications=no
RestartApplications=no
WizardStyle=modern
Compression=lzma2
SolidCompression=yes
VersionInfoVersion={#NumericVersion}
OutputDir=..\dist
OutputBaseFilename=NVIDIAReplayMonitor-{#AppVersion}-Setup

[Languages]
Name: "japanese"; MessagesFile: "compiler:Languages\Japanese.isl"

[Tasks]
Name: "startup"; Description: "Windowsログイン時に起動する"
Name: "launch"; Description: "インストール完了後に起動する"

[Files]
Source: "{#PayloadDir}\NVIDIAReplayMonitor.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PayloadDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion uninsneveruninstall

[Icons]
Name: "{#StartupDir}\NVIDIAReplayMonitor"; Filename: "{app}\NVIDIAReplayMonitor.exe"; WorkingDir: "{app}"; Tasks: startup
Name: "{#MenuDir}\NVIDIAReplayMonitor"; Filename: "{app}\NVIDIAReplayMonitor.exe"; WorkingDir: "{app}"
Name: "{#MenuDir}\アンインストール"; Filename: "{uninstallexe}"

[Code]
const
  InstallerGate = '{#GateName}';
  UninstallKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{#ProductId}_is1';

var
  GateHandle: THandle;
  PreviousDir: String;
  InstallCompleted: Boolean;

function CreateGate(Attributes: NativeInt; InitialOwner: Boolean; Name: String): THandle;
  external 'CreateMutexW@kernel32.dll stdcall';
function CloseGate(Handle: THandle): Boolean;
  external 'CloseHandle@kernel32.dll stdcall';

function AcquireInstallerGate: Boolean;
begin
  GateHandle := CreateGate(0, False, InstallerGate);
  Result := (GateHandle <> 0) and (DLLGetLastError <> 183);
  if not Result then begin
    if GateHandle <> 0 then CloseGate(GateHandle);
    GateHandle := 0;
    SuppressibleMsgBox('インストールまたはアンインストールを実行中です。完了後に再実行してください。', mbError, MB_OK, IDOK);
  end;
end;

procedure ReleaseInstallerGate;
begin
  if GateHandle <> 0 then begin
    CloseGate(GateHandle);
    GateHandle := 0;
  end;
end;

function SamePath(const Left, Right: String): Boolean;
begin
  Result := CompareText(RemoveBackslashUnlessRoot(ExpandFileName(Left)),
    RemoveBackslashUnlessRoot(ExpandFileName(Right))) = 0;
end;

function StartupPath: String;
begin
  Result := ExpandConstant('{#StartupDir}\NVIDIAReplayMonitor.lnk');
end;

function ValidateStartupShortcut: String;
var
  Shell, Shortcut: Variant;
  Target: String;
begin
  Result := '';
  if DirExists(StartupPath) then begin
    Result := '自動起動ショートカットの場所にフォルダがあります。名前を変更してから再実行してください。';
    exit;
  end;
  if not FileExists(StartupPath) then exit;
  try
    Shell := CreateOleObject('WScript.Shell');
    Shortcut := Shell.CreateShortcut(StartupPath);
    Target := Shortcut.TargetPath;
    if (Shortcut.Arguments <> '') or
       not (SamePath(Target, ExpandConstant('{app}\NVIDIAReplayMonitor.exe')) or
         ((PreviousDir <> '') and SamePath(Target, AddBackslash(PreviousDir) + 'NVIDIAReplayMonitor.exe')) or
         SamePath(Target, ExpandConstant('{localappdata}\NVIDIAReplayMonitor\NVIDIAReplayMonitor.exe'))) then
      Result := '同名の自動起動ショートカットが別の場所を指しています。上書きせず中止しました。';
  except
    Result := '自動起動ショートカットのリンク先を確認できません。' + #13#10 + GetExceptionMessage;
  end;
end;

function RunLifecycleCommand(const ExePath, Action: String; var Details: String): Boolean;
var
  Code: Integer;
  ReportPath: String;
  ReportBytes: AnsiString;
begin
  Result := False;
  Details := '';
  ReportPath := ExpandConstant('{tmp}\lifecycle-result.txt');
  if FileExists(ReportPath) and not DeleteFile(ReportPath) then begin
    Details := '前回の操作結果を更新できません。';
    exit;
  end;
  if not Exec(ExePath, Action + ' --output ' + AddQuotes(ReportPath), '', SW_HIDE,
    ewWaitUntilTerminated, Code) then begin
    Details := '処理を起動できません。' + #13#10 + SysErrorMessage(Code);
    exit;
  end;
  if LoadStringFromFile(ReportPath, ReportBytes) then
    Details := UTF8Decode(ReportBytes)
  else begin
    Details := '操作結果を確認できません。ファイルは削除していません。';
    exit;
  end;
  Result := Code = 0;
end;

function InitializeSetup: Boolean;
begin
  Result := AcquireInstallerGate;
  if Result then
    RegQueryStringValue(HKCU64, UninstallKey, 'InstallLocation', PreviousDir);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  if (PreviousDir <> '') and not SamePath(WizardDirValue, PreviousDir) then begin
    Result := '更新時は既存のインストール先を使用してください。場所を変える場合は先にアンインストールしてください。';
    exit;
  end;
  Result := ValidateStartupShortcut;
  if Result <> '' then exit;
  ExtractTemporaryFile('NVIDIAReplayMonitor.exe');
  if not RunLifecycleCommand(ExpandConstant('{tmp}\NVIDIAReplayMonitor.exe'), '--stop', Result) then begin
    Result := '監視を停止できないため、インストールを中止しました。' + #13#10 + Result;
    exit;
  end;
  Result := '';
end;

procedure RemoveLegacyUninstaller;
var
  Path, Hash: String;
begin
  Path := ExpandConstant('{app}\uninstall.cmd');
  if not FileExists(Path) then exit;
  Hash := GetSHA256OfFile(Path);
  { Only the published ZIP and the locally tested predecessor are ours. }
  if (CompareText(Hash, 'abb4eee4523fd26d371b66a46abae6e6fe73ba334c4e89cc31869829d455bd9a') = 0) or
     (CompareText(Hash, 'd71a582eba7e10a5847af90791a69c2eb4d8a090f04fdcc3ec8d15da02da57b9') = 0) then
    if not DeleteFile(Path) then RaiseException('旧アンインストールファイルを削除できません。');
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then begin
    if not WizardIsTaskSelected('startup') and FileExists(StartupPath) then begin
      if ValidateStartupShortcut <> '' then RaiseException('自動起動ショートカットが変更されたため、処理を中止しました。');
      if not DeleteFile(StartupPath) then RaiseException('自動起動ショートカットを解除できません。');
    end;
    RemoveLegacyUninstaller;
  end;
  if CurStep = ssDone then InstallCompleted := True;
end;

procedure DeinitializeSetup;
var
  Code: Integer;
begin
  ReleaseInstallerGate;
  if InstallCompleted and WizardIsTaskSelected('launch') then
    if not Exec(ExpandConstant('{app}\NVIDIAReplayMonitor.exe'), '', ExpandConstant('{app}'),
      SW_HIDE, ewNoWait, Code) then
      SuppressibleMsgBox('インストールは完了しましたが、監視を起動できません。配置先のexeから起動してください。', mbError, MB_OK, IDOK);
end;

function InitializeUninstall: Boolean;
begin
  Result := AcquireInstallerGate;
end;

procedure CurUninstallStepChanged(Step: TUninstallStep);
var
  Details, Path: String;
  Retried: Boolean;
begin
  if Step = usUninstall then begin
    Retried := False;
    while True do begin
      Details := ValidateStartupShortcut;
      if Details = '' then
        if RunLifecycleCommand(ExpandConstant('{app}\NVIDIAReplayMonitor.exe'), '--uninstall', Details) then begin
          if Pos('注意:', Details) > 0 then
            SuppressibleMsgBox(Details, mbInformation, MB_OK, IDOK);
          if Retried then
            SuppressibleMsgBox('設定の復元が完了しました。NVIDIA オーバーレイを手動でオフにした場合は、削除完了後にNVIDIA Appの設定からオンに戻してください。', mbInformation, MB_OK, IDOK);
          exit;
        end;
      if SuppressibleMsgBox('設定を復元できないため、ファイルは削除していません。' + #13#10 + Details + #13#10#13#10 +
        '原因を解消して「再試行」を押すか、「キャンセル」で中止してください。',
        mbError, MB_RETRYCANCEL, IDCANCEL) <> IDRETRY then Abort;
      Retried := True;
    end;
  end;
  if Step = usPostUninstall then begin
    Path := ExpandConstant('{app}\README.md');
    if FileExists(Path) and (CompareText(GetSHA256OfFile(Path), '{#ReadmeSHA256}') = 0) then
      DeleteFile(Path);
  end;
end;

procedure DeinitializeUninstall;
begin
  ReleaseInstallerGate;
end;
