# dbird installer for Windows.
#
#   irm https://raw.githubusercontent.com/jevido/dbird/main/install.ps1 | iex
#
# Installs the latest release to %LOCALAPPDATA%\Programs\dbird (no admin
# rights needed, and dbird can update itself there) and adds a Start menu
# shortcut. dbird needs the Microsoft Edge WebView2 runtime, which Windows 10
# and 11 include.
#
# Uninstall: & ([scriptblock]::Create((irm https://raw.githubusercontent.com/jevido/dbird/main/install.ps1))) -Uninstall
param([switch]$Uninstall)
$ErrorActionPreference = 'Stop'

$repo = if ($env:DBIRD_REPO) { $env:DBIRD_REPO } else { 'jevido/dbird' }
$dir = Join-Path $env:LOCALAPPDATA 'Programs\dbird'
$shortcut = Join-Path ([Environment]::GetFolderPath('Programs')) 'dbird.lnk'

if ($Uninstall) {
    Get-Process dbird -ErrorAction SilentlyContinue | Stop-Process -Force
    Remove-Item -Recurse -Force $dir, $shortcut -ErrorAction SilentlyContinue
    Write-Host 'dbird removed. Your connections and tabs are kept in %APPDATA%\dbird.'
    return
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("dbird-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host 'Downloading dbird for Windows...'
    $zip = Join-Path $tmp 'dbird.zip'
    Invoke-WebRequest -UseBasicParsing "https://github.com/$repo/releases/latest/download/dbird-windows-amd64.zip" -OutFile $zip
    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    Get-Process dbird -ErrorAction SilentlyContinue | Stop-Process -Force
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Copy-Item (Join-Path $tmp 'dbird.exe') (Join-Path $dir 'dbird.exe') -Force

    $shell = New-Object -ComObject WScript.Shell
    $link = $shell.CreateShortcut($shortcut)
    $link.TargetPath = Join-Path $dir 'dbird.exe'
    $link.WorkingDirectory = $dir
    $link.Description = 'Lightweight SQL client'
    $link.Save()

    Write-Host 'dbird is installed. Start it from the Start menu.'
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
