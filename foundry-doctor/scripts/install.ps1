param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [string]$InstallDir = "$HOME\\bin"
)

$Repo = if ($env:FOUNDRY_DOCTOR_REPO) { $env:FOUNDRY_DOCTOR_REPO } else { "ruairispain/copilot-web" }
$BaseUrl = if ($env:FOUNDRY_DOCTOR_BASE_URL) { $env:FOUNDRY_DOCTOR_BASE_URL } else { "https://github.com/$Repo/releases/download/foundry-doctor-v$Version" }

$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()) {
    "x64" { "amd64" }
    "arm64" { "arm64" }
    default { throw "Unsupported architecture: $($_)" }
}

$archive = "foundry-doctor_${Version}_windows_${arch}.zip"
$work = Join-Path $PWD ".foundry-doctor-install"
New-Item -ItemType Directory -Force -Path $work | Out-Null

Invoke-WebRequest -Uri "$BaseUrl/checksums.txt" -OutFile (Join-Path $work "checksums.txt")
Invoke-WebRequest -Uri "$BaseUrl/$archive" -OutFile (Join-Path $work $archive)

$expected = Select-String -Path (Join-Path $work "checksums.txt") -Pattern [regex]::Escape($archive) | ForEach-Object {
    ($_ -split '\s+')[0].ToLowerInvariant()
} | Select-Object -First 1
if (-not $expected) { throw "Checksum entry not found for $archive" }

$actual = (Get-FileHash -Algorithm SHA256 (Join-Path $work $archive)).Hash.ToLowerInvariant()
if ($actual -ne $expected) { throw "Checksum mismatch for $archive" }

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Expand-Archive -Path (Join-Path $work $archive) -DestinationPath $work -Force
Copy-Item (Join-Path $work "foundry-doctor.exe") (Join-Path $InstallDir "foundry-doctor.exe") -Force
Write-Host "Installed foundry-doctor $Version to $(Join-Path $InstallDir 'foundry-doctor.exe')"
