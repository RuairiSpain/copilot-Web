param(
    # Release tag suffix, for example v0.1.0 (the release tag is foundry-doctor/v0.1.0).
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$InstallDir = "$HOME\bin"
)

# Installs the Windows binary published by foundry-doctor-release.yml and checks it against SHA256SUMS from the same
# release. Layout: foundry-doctor_<version>_windows_<arch>.exe, SHA256SUMS, tag foundry-doctor/<version>.
$ErrorActionPreference = "Stop"

$Repo = if ($env:FOUNDRY_DOCTOR_REPO) { $env:FOUNDRY_DOCTOR_REPO } else { "RuairiSpain/copilot-Web" }
$BaseUrl = if ($env:FOUNDRY_DOCTOR_BASE_URL) { $env:FOUNDRY_DOCTOR_BASE_URL } else { "https://github.com/$Repo/releases/download/foundry-doctor/$Version" }

$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()) {
    "x64" { "amd64" }
    "arm64" { "arm64" }
    default { throw "Unsupported architecture: $($_)" }
}

$asset = "foundry-doctor_${Version}_windows_${arch}.exe"
$work = Join-Path $PWD ".foundry-doctor-install"
New-Item -ItemType Directory -Force -Path $work | Out-Null

Invoke-WebRequest -Uri "$BaseUrl/SHA256SUMS" -OutFile (Join-Path $work "SHA256SUMS")
Invoke-WebRequest -Uri "$BaseUrl/$asset" -OutFile (Join-Path $work $asset)

# Lines look like "<hash>  <name>" or "<hash> *<name>".
$expected = Get-Content (Join-Path $work "SHA256SUMS") | ForEach-Object {
    $parts = $_ -split '\s+', 2
    if ($parts.Count -eq 2 -and $parts[1].TrimStart('*') -eq $asset) { $parts[0].ToLowerInvariant() }
} | Select-Object -First 1
if (-not $expected) { throw "Checksum entry not found for $asset" }

$actual = (Get-FileHash -Algorithm SHA256 (Join-Path $work $asset)).Hash.ToLowerInvariant()
if ($actual -ne $expected) { throw "Checksum mismatch for $asset" }

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item (Join-Path $work $asset) (Join-Path $InstallDir "foundry-doctor.exe") -Force
Write-Host "Installed foundry-doctor $Version to $(Join-Path $InstallDir 'foundry-doctor.exe')"
