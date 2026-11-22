# Cubit: instalador de Windows por PowerShell (sin administrador). / Cubit PowerShell installer (no admin).
#
#   irm https://github.com/eynort/cubit/releases/latest/download/install.ps1 | iex
#   & ([scriptblock]::Create((irm https://github.com/eynort/cubit/releases/latest/download/install.ps1))) -Uninstall
#
# Descarga el binario de la última versión, comprueba su SHA256 contra SHA256SUMS, lo instala en
# %LOCALAPPDATA%\Cubit\bin, guarda los datos en %LOCALAPPDATA%\Cubit y crea un acceso en el menú Inicio.
# -Version v1.2.3 instala una versión concreta; -Uninstall lo quita (con -Purge, también los datos).
param(
    [switch]$Uninstall,
    [switch]$Purge,
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$Repo = "eynort/cubit"
$Root = Join-Path $env:LOCALAPPDATA "Cubit"
$BinDir = Join-Path $Root "bin"
$Exe = Join-Path $BinDir "cubit.exe"
$Lnk = Join-Path ([Environment]::GetFolderPath("Programs")) "Cubit.lnk"

if ($Uninstall) {
    Get-Process cubit -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $Exe } | Stop-Process -Force
    Remove-Item -Force -ErrorAction SilentlyContinue $Lnk
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $BinDir
    Write-Host "Cubit uninstalled."
    if ($Purge) {
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $Root
        Write-Host "Data deleted: $Root"
    } else {
        Write-Host "Your data is still in $Root (use -Uninstall -Purge to delete it)."
    }
    return
}

$arch = $env:PROCESSOR_ARCHITECTURE
if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
switch ($arch) {
    "AMD64" { $goarch = "amd64" }
    "ARM64" { $goarch = "arm64" }
    default { throw "Unsupported architecture: $arch" }
}

if ($Version) {
    $base = "https://github.com/$Repo/releases/download/$Version"
} else {
    $base = "https://github.com/$Repo/releases/latest/download"
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("cubit-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host "Downloading checksums..."
    $sums = (Invoke-WebRequest -UseBasicParsing "$base/SHA256SUMS").Content -split "`n"
    $name = $null
    $want = $null
    foreach ($line in $sums) {
        $p = $line.Trim() -split "\s+", 2
        if ($p.Count -eq 2) {
            $f = $p[1].TrimStart("*")
            if ($f -like "cubit-*-windows-$goarch.exe") { $want = $p[0]; $name = $f; break }
        }
    }
    if (-not $name) { throw "No windows-$goarch binary in this release" }

    Write-Host "Downloading $name..."
    $file = Join-Path $tmp "cubit.exe"
    Invoke-WebRequest -UseBasicParsing "$base/$name" -OutFile $file
    $got = (Get-FileHash -Algorithm SHA256 $file).Hash.ToLower()
    if ($got -ne $want.ToLower()) { throw "SHA256 mismatch for $name (expected $want, got $got); nothing was installed" }

    Get-Process cubit -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $Exe } | Stop-Process -Force
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    Copy-Item -Force $file $Exe

    $sh = New-Object -ComObject WScript.Shell
    $s = $sh.CreateShortcut($Lnk)
    $s.TargetPath = $Exe
    $s.Arguments = "--hub :8080 --open --data `"$Root`""
    $s.WorkingDirectory = $Root
    $s.IconLocation = "$Exe,0"
    $s.WindowStyle = 7
    $s.Description = "Cubit: a Minecraft bot that fights, digs and moves"
    $s.Save()
} finally {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $tmp
}

Write-Host ""
Write-Host "Cubit installed in $Exe (data: $Root)"
Write-Host "Start it from the Start menu ('Cubit'), or run:"
Write-Host "  & '$Exe' --hub :8080 --open --data '$Root'"
$hubJson = Join-Path $Root "hub.json"
if (Test-Path $hubJson) {
    $t = (Get-Content -Raw $hubJson | ConvertFrom-Json).token
    if ($t) { Write-Host "Your panel: http://localhost:8080/?t=$t" }
} else {
    Write-Host "The panel opens in your browser (its URL, with your access key, is printed in the window)."
}
Write-Host "Windows may show a SmartScreen warning the first time: More info -> Run anyway (the binary is not signed)."
