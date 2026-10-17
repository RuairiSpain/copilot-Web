<#
.SYNOPSIS
    Runs the foundry-rules fixtures through PSRule and asserts the results listed in tests/expected.md.

.DESCRIPTION
    Needs PSRule.Rules.Azure (which brings PSRule). Runs every fixture from the foundry-rules folder with
    -Baseline Foundry.Prod -Module PSRule.Rules.Azure. Exit code 1 on any failed assertion, 0 otherwise.

    Assertions (see expected.md):
      - "Must fail":      the rule reports Fail for the named resource (azure.yaml fixtures: for the file).
      - "Must not fail":  the rule does not report Fail on the fixture. On the fixtures where expected.md says
                          "no Foundry rule with status evaluated-subset reports Fail" (azure.good, azure.edge,
                          arm.good) every evaluated-subset rule is checked, not only the ones named.
      - NotEvaluated:     rules with status 'not-evaluated' produce no Pass, Fail or Error on any fixture.
      - Any Error outcome from a Foundry rule is a failure (the rule threw).
    Also a guard against vacuous passes: each fixture must produce at least one Foundry rule result.

    The assertions are per rule and resource. They cannot tell which value inside a resource caused a failure
    (for example the two ranges in FND-SEC-006), so those parentheticals in expected.md are not asserted.
#>
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $root

Import-Module PSRule.Rules.Azure -ErrorAction Stop
$mod = Get-Module PSRule.Rules.Azure
Write-Host ("PSRule.Rules.Azure {0}; PowerShell {1}; {2}" -f $mod.Version, $PSVersionTable.PSVersion, [System.Runtime.InteropServices.RuntimeInformation]::OSDescription)

# Rule name -> status tag ('evaluated-subset' | 'not-evaluated'), read from the rule files (no PSRule needed).
$status = @{}
$current = $null
foreach ($line in Get-Content -LiteralPath (Join-Path $root '.ps-rule/Foundry.Rule.yaml')) {
    if ($line -match '^  name: (FND-[A-Z]+-\d+)\s*$') { $current = $Matches[1] }
    elseif ($current -and $line -match '^    status: (\S+)\s*$') { $status[$current] = $Matches[1]; $current = $null }
}
foreach ($line in Get-Content -LiteralPath (Join-Path $root '.ps-rule/Foundry.Rule.ps1')) {
    if ($line -match "^Rule '(FND-[A-Z]+-\d+)'.*status = '([^']+)'") { $status[$Matches[1]] = $Matches[2] }
}
if ($status.Count -ne 108) { throw "Expected 108 rule status entries from the rule files, found $($status.Count)." }

# Fixture definitions. Fail: @{ Target = <resource name or '*'>; Rules = ... }. NoFail: @{ Target; Rules }.
$fixtures = @(
    @{ File = 'tests/azure.good.yaml'; Strict = @(); NoFail = @(@{ Target = '*'; Rules = 'FND-CFG-002', 'FND-CFG-003', 'FND-CFG-008', 'FND-CFG-011' }) }
    @{ File = 'tests/azure.edge.yaml'; Strict = @(); NoFail = @(@{ Target = '*'; Rules = 'FND-CFG-002', 'FND-CFG-003' }) }
    @{ File = 'tests/azure.bad.yaml'; Fail = @(@{ Target = '*'; Rules = 'FND-CFG-002', 'FND-CFG-003', 'FND-CFG-008', 'FND-CFG-011' }) }
    @{ File = 'tests/arm.good.json'; Strict = @('FND-COST-002'); Fail = @(@{ Target = '*'; Rules = 'FND-COST-002' }) }
    @{ File = 'tests/arm.bad.foundry-search.json'; Fail = @(
            @{ Target = 'foundrybad01'; Rules = 'FND-SEC-001', 'FND-NET-001', 'FND-IDN-001', 'FND-SEC-010', 'FND-SEC-011' }
            @{ Target = 'foundrybad01/proj1'; Rules = 'FND-IDN-001' }
            @{ Target = 'search-bad'; Rules = 'FND-SEC-002', 'FND-REL-001', 'FND-SEC-010' }
            @{ Target = 'search-free-bad'; Rules = 'FND-IQ-012', 'FND-SEC-002' }) }
    @{ File = 'tests/arm.bad.storage-cosmos-kv.json'; Fail = @(
            @{ Target = 'stfoundrybad01'; Rules = 'FND-SEC-004', 'FND-REL-002', 'FND-SEC-006', 'FND-SEC-010' }
            @{ Target = 'cosmos-foundry-bad'; Rules = 'FND-SEC-003', 'FND-REL-003', 'FND-SEC-006', 'FND-SEC-010' }
            @{ Target = 'kv-foundry-bad'; Rules = 'FND-SEC-005' }) }
    @{ File = 'tests/arm.bad.deployments-apim.json'
        Fail = @(
            @{ Target = 'foundrybad01/chat-ptu'; Rules = 'FND-REL-008', 'FND-COST-002', 'FND-CFG-010' }
            @{ Target = 'apim-foundry-bad'; Rules = 'FND-NET-009' }
            @{ Target = 'apim-foundry-dev'; Rules = 'FND-COST-002' }
            @{ Target = 'apim-foundry-bad/foundry-backend'; Rules = 'FND-REL-006' })
        NoFail = @(@{ Target = 'apim-foundry-dev'; Rules = 'FND-NET-009' }) }
    @{ File = 'tests/arm.bad.identity.json'; Fail = @(
            @{ Target = '33333333-3333-3333-3333-333333333333'; Rules = 'FND-IDN-003' }
            @{ Target = 'deploy-id/github-bad'; Rules = 'FND-IDN-006' }
            @{ Target = 'foundrybad02'; Rules = 'FND-IDN-001', 'FND-SEC-001', 'FND-NET-001' }) }
    @{ File = 'tests/arm.bad.names.json'; Fail = @(
            @{ Target = 'x'; Rules = 'FND-OPS-005' }
            @{ Target = 'My-Storage'; Rules = 'FND-OPS-005' }
            @{ Target = 'kv--bad'; Rules = 'FND-OPS-005' }
            @{ Target = 'Search_Bad'; Rules = 'FND-OPS-005' }
            @{ Target = 'Cosmos_Bad'; Rules = 'FND-OPS-005' }
            @{ Target = 'ab'; Rules = 'FND-OPS-005' }
            @{ Target = 'a'; Rules = 'FND-OPS-005' }) }
    @{ File = 'tests/arm.bad.search-sizing.json'; Fail = @(
            @{ Target = 'search-size-a'; Rules = 'FND-REL-005' }
            @{ Target = 'search-size-b'; Rules = 'FND-REL-005' }
            @{ Target = 'search-size-c'; Rules = 'FND-REL-005' }
            @{ Target = 'search-size-d'; Rules = 'FND-REL-005' }) }
)

$rows = [System.Collections.Generic.List[object]]::new()
function Add-Row($Fixture, $Rule, $Target, $Expect, $Actual, [bool]$Ok) {
    $rows.Add([pscustomobject]@{ Fixture = (Split-Path $Fixture -Leaf); Rule = $Rule; Target = $Target; Expect = $Expect; Actual = $Actual; Result = $(if ($Ok) { 'ok' } else { 'FAIL' }) })
}

foreach ($fx in $fixtures) {
    $file = $fx.File
    $recs = @(Invoke-PSRule -InputPath $file -Format Detect -Baseline Foundry.Prod -Module PSRule.Rules.Azure -Outcome All -ErrorAction Stop |
            Where-Object { $_.RuleName -like 'FND-*' })
    Write-Host ("{0}: {1} Foundry rule results" -f $file, $recs.Count)

    Add-Row $file '(any)' '-' 'results > 0' "$($recs.Count) results" ($recs.Count -gt 0)

    foreach ($r in @($recs | Where-Object { $_.Outcome -eq 'Error' })) {
        Add-Row $file $r.RuleName $r.TargetName 'no Error' 'Error' $false
    }
    foreach ($r in @($recs | Where-Object { $status[$_.RuleName] -eq 'not-evaluated' -and $_.Outcome -in 'Pass', 'Fail' })) {
        Add-Row $file $r.RuleName $r.TargetName 'no result' $r.Outcome $false
    }

    $failed = @($recs | Where-Object { $_.Outcome -eq 'Fail' })
    $match = { param($rec, $target) $target -eq '*' -or $rec.TargetName -ieq $target }

    if ($fx.ContainsKey('Fail')) {
        foreach ($e in $fx.Fail) {
            foreach ($rule in $e.Rules) {
                $hit = @($failed | Where-Object { $_.RuleName -eq $rule -and (& $match $_ $e.Target) })
                $actual = if ($hit.Count -gt 0) { 'Fail' } else {
                    $other = @($recs | Where-Object { $_.RuleName -eq $rule -and (& $match $_ $e.Target) } | ForEach-Object { $_.Outcome } | Sort-Object -Unique)
                    if ($other) { $other -join ',' } else { 'no result' }
                }
                Add-Row $file $rule $e.Target 'Fail' $actual ($hit.Count -gt 0)
            }
        }
    }
    if ($fx.ContainsKey('NoFail')) {
        foreach ($e in $fx.NoFail) {
            foreach ($rule in $e.Rules) {
                $hit = @($failed | Where-Object { $_.RuleName -eq $rule -and (& $match $_ $e.Target) })
                Add-Row $file $rule $e.Target 'not Fail' $(if ($hit.Count -gt 0) { 'Fail' } else { 'not Fail' }) ($hit.Count -eq 0)
            }
        }
    }
    if ($fx.ContainsKey('Strict')) {
        # No evaluated-subset rule other than the allowed ones may fail anywhere on this fixture.
        $allowed = @($fx.Strict)
        $bad = @($failed | Where-Object { $status[$_.RuleName] -eq 'evaluated-subset' -and $_.RuleName -notin $allowed })
        if ($bad.Count -eq 0) {
            Add-Row $file '(all evaluated-subset)' '*' ("no Fail except: " + ($allowed -join ',')) 'none' $true
        }
        else {
            foreach ($r in $bad) { Add-Row $file $r.RuleName $r.TargetName 'not Fail' 'Fail' $false }
        }
        $passes = @($recs | Where-Object { $_.Outcome -eq 'Pass' })
        Add-Row $file '(any)' '-' 'at least one Pass' "$($passes.Count) Pass" ($passes.Count -gt 0)
    }
}

$rows | Format-Table -AutoSize -Wrap | Out-String -Width 250 | Write-Host
$bad = @($rows | Where-Object { $_.Result -eq 'FAIL' })
Write-Host ("Assertions: {0}; failed: {1}" -f $rows.Count, $bad.Count)
if ($bad.Count -gt 0) {
    Write-Host 'FAILED assertions:'
    $bad | Format-Table -AutoSize -Wrap | Out-String -Width 250 | Write-Host
    exit 1
}
Write-Host 'All fixture assertions passed.'
exit 0
