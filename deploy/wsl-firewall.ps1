# Run once in an admin PowerShell when Roxy Server runs inside WSL2 (mirrored networking).
# WSL's Hyper-V firewall blocks inbound LAN traffic by default; this opens Roxy's port:
# TCP 8096 for the apps, UDP 8096 for the login screen's server discovery.
$wsl = '{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}' # WSL's VM creator id
foreach ($p in 'TCP', 'UDP') {
    Remove-NetFirewallHyperVRule -Name "Roxy$p" -ErrorAction SilentlyContinue
    New-NetFirewallHyperVRule -Name "Roxy$p" -DisplayName "Roxy server (WSL) $p 8096" -Direction Inbound `
        -VMCreatorId $wsl -Protocol $p -LocalPorts 8096 -Action Allow | Out-Null
}
Get-NetFirewallHyperVRule -Name 'Roxy*' | Format-Table Name, Enabled, Action
