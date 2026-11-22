; Cubit: instalador de Windows (Inno Setup 6.1 o más nuevo).
;
;   iscc /DAppVersion=1.2.3 /DExePath=C:\ruta\cubit.exe packaging\windows\cubit.iss
;
; Deja el instalador en dist\cubit-<versión>-windows-setup.exe.
; Instala cubit.exe en Archivos de programa\Cubit y guarda los datos
; (hub.json, bots\...) en %LOCALAPPDATA%\Cubit, que se pasan con --data.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef ExePath
  #define ExePath "..\..\dist\cubit.exe"
#endif

#define AppName "Cubit"
#define Panel "--hub :8080 --open --data ""{localappdata}\Cubit"""

[Setup]
AppId={{6D1F3C52-7B7A-4C1E-9B1A-C0B17A5E2F10}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher=eynort
AppPublisherURL=https://github.com/eynort/cubit
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\..\dist
OutputBaseFilename=cubit-{#AppVersion}-windows-setup
SetupIconFile=cubit.ico
UninstallDisplayIcon={app}\cubit.exe
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
LicenseFile=..\..\LICENSE

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Shortcuts:"
Name: "autostart"; Description: "Start Cubit when I log in"; GroupDescription: "Startup:"; Flags: unchecked
Name: "ollama"; Description: "Local AI (Ollama): download and install it from ollama.com"; GroupDescription: "Optional:"; Flags: unchecked

[Files]
Source: "{#ExePath}"; DestDir: "{app}"; DestName: "cubit.exe"; Flags: ignoreversion

[Dirs]
Name: "{localappdata}\Cubit"

[Icons]
Name: "{group}\Cubit"; Filename: "{app}\cubit.exe"; Parameters: "{#Panel}"; WorkingDir: "{localappdata}\Cubit"; IconFilename: "{app}\cubit.exe"; Flags: runminimized
Name: "{group}\Uninstall Cubit"; Filename: "{uninstallexe}"
Name: "{autodesktop}\Cubit"; Filename: "{app}\cubit.exe"; Parameters: "{#Panel}"; WorkingDir: "{localappdata}\Cubit"; Tasks: desktopicon; Flags: runminimized
Name: "{userstartup}\Cubit"; Filename: "{app}\cubit.exe"; Parameters: "--hub :8080 --data ""{localappdata}\Cubit"""; WorkingDir: "{localappdata}\Cubit"; Tasks: autostart; Flags: runminimized

[Run]
Filename: "{tmp}\OllamaSetup.exe"; Parameters: "/SILENT"; StatusMsg: "Installing Ollama..."; Tasks: ollama; Check: OllamaDownloaded; Flags: waituntilterminated
Filename: "{app}\cubit.exe"; Parameters: "{#Panel}"; WorkingDir: "{localappdata}\Cubit"; Description: "Open Cubit now"; Flags: postinstall nowait skipifsilent runminimized

[Code]
var
  DownloadPage: TDownloadWizardPage;
  OllamaOk: Boolean;

function OllamaDownloaded: Boolean;
begin
  Result := OllamaOk;
end;

procedure InitializeWizard;
begin
  DownloadPage := CreateDownloadPage(SetupMessage(msgWizardPreparing), SetupMessage(msgPreparingDesc), nil);
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if (CurPageID = wpReady) and WizardIsTaskSelected('ollama') then
  begin
    DownloadPage.Clear;
    { el instalador oficial de Ollama }
    DownloadPage.Add('https://ollama.com/download/OllamaSetup.exe', 'OllamaSetup.exe', '');
    DownloadPage.Show;
    try
      try
        DownloadPage.Download;
        OllamaOk := True;
      except
        OllamaOk := False;
        if not DownloadPage.AbortedByUser then
          SuppressibleMsgBox('Could not download Ollama (' + GetExceptionMessage + '). Cubit will be installed without it; you can get Ollama later from https://ollama.com/download', mbError, MB_OK, IDOK);
      end;
    finally
      DownloadPage.Hide;
    end;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Dir: String;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    Dir := ExpandConstant('{localappdata}\Cubit');
    if DirExists(Dir) and (not UninstallSilent) then
      if MsgBox('Also delete your Cubit data (bots, settings, worlds) in ' + Dir + '?' + #13#10 + 'Choose No to keep it.',
                mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
        DelTree(Dir, True, True, True);
  end;
end;
