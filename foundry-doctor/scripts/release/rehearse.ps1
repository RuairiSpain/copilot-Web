param(
    [Parameter(Mandatory = $true)]
    [string]$Version
)

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$env:GOTOOLCHAIN = if ($env:GOTOOLCHAIN) { $env:GOTOOLCHAIN } else { "local" }

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "go must be on PATH before running release rehearsal"
}
if (-not (Get-Command python -ErrorAction SilentlyContinue)) {
    throw "python must be on PATH before running release rehearsal"
}

Push-Location $root
try {
    bash "$root/scripts/release/rehearse.sh" $Version
} finally {
    Pop-Location
}
