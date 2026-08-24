#!/usr/bin/env pwsh
# urnet-tools: URnetwork provider manager
# Author: full-bars (GitHub), onlyinthe707 / "mesocyclone" (Discord)
# Based on: Ar Rakin, Ryan Mello (original)
# https://github.com/full-bars/urnetwork-3.23-fix

<#
.SYNOPSIS
    URnetwork provider manager and toolkit.

.DESCRIPTION
    This script helps you manage your URnetwork provider installation.
    Manages the provider process, proxy configuration, and
    runtime tuning.

.PARAMETER Command
    The subcommand to execute (first positional argument).
    
    Core Commands:
      start                       Start the provider
      stop                        Stop the provider
      restart                     Restart the provider
      update                      Upgrade to the latest version
      status                      Show provider service status
      version                     Show installed URnetwork version
      logs [-n <lines>]           Show provider logs (-n to limit lines)
    
    Maintenance:
      uninstall                   Uninstall URnetwork
      reinstall                   Reinstall URnetwork
      auto-update-enable          Enable auto-updates
      auto-update-disable         Disable auto-updates
      auto-update-freq <day|week|month>
                                  Change the frequency of auto-update checks
      auto-start-enable           Enable auto-start on login
      auto-start-disable          Disable auto-start on login
    
    Performance & Tuning:
      hot-restart <on|off>        Reuse client JWT identities across restarts
    
    Proxy Management:
      proxy refresh               Re-read configs and hot-reload proxies
      proxy remove-dead [--degraded] [--auth-failures=<N>] [--yes] [--preview]
                                  Interactively prune dead/degraded proxies
      proxy summary               Fleet summary (sources, health, counts)
      proxy remove --match=<pattern> [--yes] [--preview]
                                  Remove proxies matching a host pattern
      proxy exclude [<pattern>] [--remove]
                                  Exclude patterns from proxy discovery
      choose_network <api_url> <connect_url>
                                  Save a custom API/connect backend
      choose_network --reset      Clear saved custom network, revert to main network
    
    Run 'urnet-tools.ps1 <command>' without arguments for usage details.

.PARAMETER InstalledPath
    The path where URnetwork provider was installed. Defaults to
    %LOCALAPPDATA%\urnetwork\provider on Windows.

.PARAMETER NoConfirm
    Skip confirmation prompts (use with caution).

.PARAMETER Help
    Show this help and exit.

.EXAMPLE
    urnet-tools.ps1 update
    Updates URnetwork to the latest version.

.EXAMPLE
    urnet-tools.ps1 hot-restart on
    Enables client JWT reuse across restarts.

.OUTPUTS
    String. Status messages, logs, and command output.

.INPUTS
    None. Does not take any input.

.LINK
    https://github.com/full-bars/urnetwork-3.23-fix
#>

param(
    [Parameter(Position = 0)]
    [ValidateSet("uninstall", "update", "start", "stop", "restart", "status", "version", "reinstall", "auto-update-enable", "auto-update-disable", "auto-update-freq", "auto-start-enable", "auto-start-disable", "proxy", "logs", "hot-restart", "self-heal", "choose_network")]
    [String]$Command,
    [Switch]$Help = $false,
    [String]$InstalledPath = "",
    [Switch]$NoConfirm = $false,
    [String]$ContainerName = "urnetwork",
    [Parameter(ValueFromRemainingArguments = $true)]
    [String[]]$SubArgs
);

if ($Help) {
    Get-Help $MyInvocation.MyCommand.Path -Full
    exit 0
}

if (-not $Command) {
    Write-Error "Please specify a command!"
    exit 1
}

if (-not $InstalledPath) {
    $InstalledPath = Split-Path -Path $MyInvocation.MyCommand.Path
}

$BinarySuffix = ""

if (-not $IsLinux) {
    $BinarySuffix = ".exe"
}

$GithubURLBase = "https://api.github.com/repos/full-bars/urnetwork-3.23-fix"

function Get-Path {
    return [Environment]::GetEnvironmentVariable("PATH", [System.EnvironmentVariableTarget]::User)
}

function Set-Path {
    param (
        [Parameter(Mandatory = $true)]
        [String]$Value
    )

    [Environment]::SetEnvironmentVariable("PATH", $Value, [System.EnvironmentVariableTarget]::User)
}

$VersionPath = Join-Path $InstalledPath -ChildPath "version"
$InstallDatePath = Join-Path $InstalledPath -ChildPath "date"

function Check-Update {
    $ReleaseInfo = $null
    try {
        $ReleaseInfo = Invoke-RestMethod -Uri "$GithubURLBase/releases/latest"
    }
    catch {}

    $InstalledTag = ([String](Get-Content $VersionPath -Raw)).Trim()

    if ($ReleaseInfo) {
        $Tag = $ReleaseInfo.tag_name
        $PublishedAt = [DateTime]$ReleaseInfo.published_at
        $InstallDate = [DateTime](([String](Get-Content $InstallDatePath -Raw)).Trim())

        if (-not $? -or -not $InstallDate) {
            Write-Error "Cannot read the installation date file at $InstallDatePath"
            exit 1
        }

        if ($InstallDate -ge $PublishedAt) {
            Write-Host "Installed version is up-to-date ($InstalledTag)"
            return $null
        }

        Write-Host "Update available ($Tag)"
        return $Tag
    }

    # GitHub API unavailable: fall back to the dl.fullbars.xyz Worker. It only
    # exposes a tag (no published-date), so compare tags directly instead of
    # the install-date check used above.
    Write-Warning "GitHub API unavailable, trying dl.fullbars.xyz fallback..."
    $Tag = $null
    try {
        $Tag = (Invoke-RestMethod -Uri "https://dl.fullbars.xyz/latest-version").Trim()
    }
    catch {}

    if (-not $Tag) {
        Write-Error "Failed to fetch release information from GitHub API. Are you sure the version exists and your internet connection is working?"
        exit 1
    }

    if ($Tag -eq $InstalledTag) {
        Write-Host "Installed version is up-to-date ($InstalledTag)"
        return $null
    }

    Write-Host "Update available ($Tag)"
    return $Tag
}

function Add-StartupCommand {
    param(
	    [String]$Frequency
    );
    
    $StartupPath = Join-Path -Path $env:APPDATA -ChildPath "Microsoft\Windows\Start Menu\Programs\Startup"
    $ShortcutPath = Join-Path -Path $StartupPath -ChildPath "urnetwork-update.lnk"

    if (Test-Path $ShortcutPath) {
	    Remove-Item -Path $ShortcutPath -Force
    }

    $Arguments = '-WindowStyle Hidden -File .\urnetwork-updater.ps1 -InstalledPath "' + $InstalledPath + '" -Frequency every-' + $Frequency
    
    $WshShell = New-Object -ComObject WScript.Shell
    $Shortcut = $WshShell.CreateShortcut($ShortcutPath)
    $Shortcut.TargetPath = "powershell.exe"
    $Shortcut.Arguments = $Arguments
    $Shortcut.WorkingDirectory = $InstalledPath
    $Shortcut.WindowStyle = 7
    $Shortcut.Save()

    return "powershell.exe $Arguments"
}

function Enable-AutoUpdate {
    param(
	    [String]$Frequency = "day"
    );
    
    $StartupPath = Join-Path -Path $env:APPDATA -ChildPath "Microsoft\Windows\Start Menu\Programs\Startup"
    $ShortcutPath = Join-Path -Path $StartupPath -ChildPath "urnetwork-update.lnk"

    if (Test-Path $ShortcutPath) {
        Write-Error "Auto update is already enabled!"
        exit 1
    }

    Add-StartupCommand -Frequency $Frequency
    Start-Process -FilePath $ShortcutPath -WindowStyle Hidden
    Write-Host "Auto update enabled (frequency: every $Frequency)"
}

function Disable-AutoUpdate {
    param(
	    [Switch]$NoError = $false
    );
    
    $StartupPath = Join-Path -Path $env:APPDATA -ChildPath "Microsoft\Windows\Start Menu\Programs\Startup"
    $ShortcutPath = Join-Path -Path $StartupPath -ChildPath "urnetwork-update.lnk"

    if (-not (Test-Path $ShortcutPath)) {
        if (!$NoError) {
            Write-Error "Auto update is already disabled!"
            exit 1
        }

        return
    }

    $PIDFile = Join-Path $InstalledPath -ChildPath "urnetwork-updater.pid"

    if (Test-Path $PIDFile) {
        $UpdaterPID = ([String](Get-Content -Raw $PIDFile)).Trim()

        if ($?) {
            Remove-Item $PIDFile -Force
            Stop-Process -Id $UpdaterPID -ErrorAction SilentlyContinue
        }
    }

    Remove-Item $ShortcutPath -Force
    Write-Host "Auto update disabled"
}

$BinaryPath = Join-Path $InstalledPath -ChildPath "urnetwork$BinarySuffix"

function Enable-AutoStart {
    $StartupPath = Join-Path -Path $env:APPDATA -ChildPath "Microsoft\Windows\Start Menu\Programs\Startup"
	$ShortcutPath = Join-Path -Path $StartupPath -ChildPath "urnetwork.lnk"

	if (Test-Path $ShortcutPath) {
	    Write-Error "Auto-start is already enabled"
        exit 1
	}

	$StartCommand = "Start-Process -FilePath '$BinaryPath' -ArgumentList 'provide' -WindowStyle Hidden"
	$Arguments = '-NoProfile -WindowStyle Hidden -Command "' + $StartCommand + '"'

	Write-Host "Startup command: powershell.exe $Arguments"

	$WshShell = New-Object -ComObject WScript.Shell
	$Shortcut = $WshShell.CreateShortcut($ShortcutPath)
	$Shortcut.TargetPath = "powershell.exe"
	$Shortcut.Arguments = $Arguments
	$Shortcut.WorkingDirectory = $Destination
	$Shortcut.WindowStyle = 7
	$Shortcut.Save()
}

function Disable-AutoStart {
    $StartupPath = Join-Path -Path $env:APPDATA -ChildPath "Microsoft\Windows\Start Menu\Programs\Startup"
    $ShortcutPath = Join-Path -Path $StartupPath -ChildPath "urnetwork.lnk"
    $UpdateShortcutPath = Join-Path -Path $StartupPath -ChildPath "urnetwork-update.lnk"

    if (Test-Path $ShortcutPath) {
	    Remove-Item -Path $ShortcutPath -Force
    }

    if (Test-Path $UpdateShortcutPath) {
	    Remove-Item -Path $UpdateShortcutPath -Force
    }
}

function Do-Uninstall {
    Write-Host "Uninstalling URnetwork provider"
    Disable-AutoUpdate -NoError
    
    $SelfPath = Join-Path $InstalledPath -ChildPath "urnet-tools.ps1"
    $UpdaterPath = Join-Path $InstalledPath -ChildPath "urnetwork-updater.ps1"

    Write-Host "Removing provider executable"
    
    if (Test-Path $BinaryPath) {
	    Remove-Item -Path $BinaryPath -Force
    }

    if (Test-Path $SelfPath) {
	    Remove-Item -Path $SelfPath -Force
    }

    if (Test-Path $UpdaterPath) {
	    Remove-Item -Path $UpdaterPath -Force
    }

    if (Test-Path $VersionPath) {
	    Remove-Item -Path $VersionPath -Force
    }

    if (Test-Path $InstallDatePath) {
	    Remove-Item -Path $InstallDatePath -Force
    }

    Write-Host "Removing data directory"
    
    $DataDir = ""

    if ($OS -eq "windows") {
	    $DataDir = "$env:HOMEDRIVE$env:HOMEPATH\.urnetwork"
    }
    else {
	    $DataDir = "$env:HOME/.urnetwork"
    }

    if (Test-Path $DataDir) {
	    Remove-Item -Path $DataDir -Recurse -Force
    }

    Write-Host "Removing startup entries (if any)"
    Disable-AutoStart
    
    $EnvValue = $InstalledPath
    $EnvPath = Get-Path
    $EnvPathSplitted = $EnvPath.Split(";")

    if ($EnvPathSplitted -contains $EnvValue) {
        Write-Host "Updating %PATH%"

        $NewPath = ($EnvPathSplitted | Where-Object { $_ -ne $EnvValue -and $_ -ne "" }) -join ';'
        Set-Path -Value $NewPath

        if (!$?) {
            Write-Error "Failed to update %PATH%"
            exit 1
        }
    }
}

function Get-URnetworkProcess {
    $Process = Get-Process | Where-Object { $_.ProcessName -eq 'urnetwork' }
    return $Process
}

function Get-ProviderUptime {
    $statePath = "$env:USERPROFILE\.urnetwork\proxy.state"
    if (-not (Test-Path $statePath)) { return $null }
    try {
        $state = Get-Content $statePath | ConvertFrom-Json
        if (-not $state.started_at) { return $null }
        return (Get-Date) - [datetime]::Parse($state.started_at)
    } catch { return $null }
}

function Test-ProviderIsDocker {
    # Detect deployment model for the *provider* container (-ContainerName):
    # Docker takes priority when a matching container is running; native
    # $BinaryPath is the fallback. Factored out so "proxy",
    # "choose_network", and "logs" can use it instead of
    # assuming Docker unconditionally.
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        return $false
    }
    try {
        $job = Start-Job -ScriptBlock { docker ps -a --format '{{.Names}}' 2>$null }
        $null = Wait-Job $job -Timeout 5
        $allContainers = Receive-Job $job -ErrorAction SilentlyContinue
        Remove-Job $job -Force

        $job2 = Start-Job -ScriptBlock { docker ps --format '{{.Names}}' 2>$null }
        $null = Wait-Job $job2 -Timeout 5
        $running = Receive-Job $job2 -ErrorAction SilentlyContinue
        Remove-Job $job2 -Force

        if (($allContainers -split "`n") -contains $ContainerName) {
            if (($running -split "`n") -contains $ContainerName) {
                return $true
            } else {
                Write-Warning "Container '$ContainerName' exists but is stopped. Start it first: docker start $ContainerName"
            }
        }
    } catch {
        return $false
    }
    return $false
}

function Get-WarmProxyCount {
    $statePath = "$env:USERPROFILE\.urnetwork\proxy.state"
    if (-not (Test-Path $statePath)) { return 0 }
    try {
        $state = Get-Content $statePath | ConvertFrom-Json
        $up = ($state.proxies.PSObject.Properties.Value | Where-Object { $_.health -eq "up" }).Count
        return $up
    } catch { return 0 }
}

function Test-HotRestartEnabled {
    # Reads persistent registry state (User scope), matching
    # provider/main.go's hotRestartEnabled() — on by default unless
    # explicitly disabled. Single source of truth for the "hot-restart"
    # command's status branch and this confirmation prompt.
    $val = [Environment]::GetEnvironmentVariable("URNETWORK_HOT_RESTART",
        [System.EnvironmentVariableTarget]::User)
    return $val -ne "0"
}

function Confirm-ColdRestart {
    $uptime = Get-ProviderUptime
    if ($null -eq $uptime -or $uptime.TotalHours -lt 8) {
        $answer = Read-Host "Restart the provider? [y/N]"
        return $answer -eq "y"
    }
    $h = [int]$uptime.TotalHours
    $m = [int]($uptime.TotalMinutes % 60)
    $up = Get-WarmProxyCount

    if (Test-HotRestartEnabled) {
        Write-Host ""
        Write-Host "╔══════════════════════════════════════════════════════════════╗"
        Write-Host "║  WARNING: A WARM RESTART WILL BE ATTEMPTED                   ║"
        Write-Host "╚══════════════════════════════════════════════════════════════╝"
        Write-Host ""
        Write-Host "Provider uptime:  ${h}h ${m}m"
        Write-Host "Warmed proxies:   $up online"
        Write-Host ""
        Write-Host "Hot-restart is enabled — client JWT identities and reputation may"
        Write-Host "carry over, but this is not guaranteed (binary upgrades, internal"
        Write-Host "state resets, or other factors can still force a cold restart)."
        Write-Host ""
        $answer = Read-Host "Restart the provider? [y/N]"
        return $answer -eq "y"
    }

    Write-Host ""
    Write-Host "╔══════════════════════════════════════════════════════════════╗"
    Write-Host "║  WARNING: COLD RESTART — ALL WARMUP PROGRESS WILL BE LOST    ║"
    Write-Host "╚══════════════════════════════════════════════════════════════╝"
    Write-Host ""
    Write-Host "Provider uptime:  ${h}h ${m}m"
    Write-Host "Warmed proxies:   $up online"
    Write-Host ""
    Write-Host "Restarting will immediately disconnect all proxies and reset the"
    Write-Host "8-12h warmup period from scratch. Earning capacity will be"
    Write-Host "significantly reduced during recovery."
    Write-Host ""
    $a1 = Read-Host "Are you sure you want to discard ${h}h ${m}m of warmup progress? [y/N]"
    if ($a1 -ne "y") { return $false }
    $a2 = Read-Host "This will interrupt live traffic on $up proxies. Proceed? [y/N]"
    return $a2 -eq "y"
}

switch ($Command) {
    "uninstall" {
        Do-Uninstall
        break	
    }

    "update" {
        $Tag = Check-Update

        if (-not $Tag) {
            Write-Host "No update is available"
            exit 0
        }
        
        Write-Host "Downloading the installer script of version $Tag"
        Write-Host "Executing installer script of version $Tag"

        $TempScriptPath = Join-Path $env:TEMP -ChildPath "urnetwork-installer.ps1"
        Invoke-RestMethod "https://raw.githubusercontent.com/full-bars/urnetwork-3.23-fix/refs/heads/main/scripts/Provider_Install_Win32.ps1" -OutFile $TempScriptPath

        if (!$?) {
            Write-Error "Failed to download the installer script"
            exit 1
        }

        & $TempScriptPath -Destination $InstalledPath -NonInteractive

        if (-not $?) {
            Write-Error "Update failed"
            exit 1
        }

        Remove-Item $TempScriptPath -Force
        Write-Host "Update completed"
        break
    }

    "version" {
        $InstalledTag = ([String](Get-Content $VersionPath -Raw)).Trim()

        if (-not $? -or -not $InstalledTag) {
            Write-Error "Cannot read the version file at $VersionPath"
            exit 1
        }
        
        Write-Host "URnetwork version $InstalledTag"
        Check-Update
        break
    }

    "reinstall" {
        $InstalledTag = ([String](Get-Content $VersionPath -Raw)).Trim()

        if (-not $? -or -not $InstalledTag) {
            Write-Error "Cannot read the version file at $VersionPath"
            exit 1
        }	

        Do-Uninstall
        Write-Host "Downloading the installer script of version $InstalledTag"
        Write-Host "Executing installer script of version $InstallerTag"

        $TempScriptPath = Join-Path $env:TEMP -ChildPath "urnetwork-installer.ps1"
        Invoke-RestMethod "https://raw.githubusercontent.com/full-bars/urnetwork-3.23-fix/refs/heads/main/scripts/Provider_Install_Win32.ps1" -OutFile $TempScriptPath

        if (!$?) {
            Write-Error "Failed to download the installer script"
            exit 1
        }

        & $TempScriptPath -Destination $InstalledPath -NonInteractive -Version $InstalledTag

        if (-not $?) {
            Write-Error "Reinstallation failed"
            exit 1
        }

        Remove-Item $TempScriptPath -Force
        Write-Host "Reinstallation completed"
        break
    }

    "auto-update-enable" {
        Enable-AutoUpdate
        break
    }

    "auto-update-disable" {
        Disable-AutoUpdate
        break
    }

    "auto-update-freq" {
        $Answer = Read-Host "How frequently do you want the update checks to happen? every-[day/week/month]"
        $Frequency = switch ($Answer) {
            "every-day" { "day" }
            "day" { "day" }
            "every-week" { "week" }
            "week" { "week" }
            "every-month" { "month" }
            "month" { "month" }
            default {
                Write-Error "Invalid frequency: $Frequency"
                exit 1
            }
        }
        
        Disable-AutoUpdate
        Enable-AutoUpdate -Frequency $Frequency 
        break
    }

    "auto-start-enable" {
        Enable-AutoStart
        break
    }

    "auto-start-disable" {
        Disable-AutoStart
        break
    }

    "start" {
        Start-Process -FilePath "$BinaryPath" -ArgumentList "provide" -WindowStyle Hidden
        break
    }

    "stop" {
        if (-not $NoConfirm) {
            if (-not (Confirm-ColdRestart)) {
                Write-Host "Aborted."
                exit 0
            }
        }
        $Process = Get-URnetworkProcess
        
        if ($Process) {
            $URnetworkPID = $Process.Id
            Stop-Process -Id $URnetworkPID -ErrorAction SilentlyContinue
        }

        break
    }

    "restart" {
        if (-not $NoConfirm) {
            if (-not (Confirm-ColdRestart)) {
                Write-Host "Aborted."
                exit 0
            }
        }
        
        # Stop then start
        & $MyInvocation.MyCommand.Path stop -NoConfirm
        Start-Sleep -Seconds 2
        & $MyInvocation.MyCommand.Path start
        break
    }

    "status" {
        $Process = Get-URnetworkProcess 
        
        if ($Process) {
            Write-Host "Status: Running"
        }
        else {
            Write-Host "Status: Stopped"
        }

        break
    }

    "self-heal" {
        $mode = if ($SubArgs) { $SubArgs[0] } else { "" }
        $file = "$env:USERPROFILE\.urnetwork\proxy_self_heal"
        switch ($mode) {
            "on" {
                New-Item -ItemType Directory -Force -Path (Split-Path $file) | Out-Null
                Set-Content -Path $file -Value "on" -NoNewline
                Write-Host "Self-heal enabled (load gate + auto cleanup active)"
                break
            }
            "off" {
                New-Item -ItemType Directory -Force -Path (Split-Path $file) | Out-Null
                Set-Content -Path $file -Value "off" -NoNewline
                Write-Host "Self-heal disabled (load gate + auto cleanup turned off)"
                break
            }
            { $_ -in "status", "" } {
                if ((Test-Path $file) -and (Get-Content $file -Raw).Trim() -eq "on") {
                    Write-Host "self-heal: on"
                } elseif (Test-Path $file) {
                    Write-Host "self-heal: off"
                } else {
                    Write-Host "self-heal: off (default; enable with 'urnet-tools self-heal on' or URNETWORK_SELF_HEAL=1)"
                }
                $pressureFile = "$env:USERPROFILE\.urnetwork\pressure_status"
                if (Test-Path $pressureFile) {
                    try {
                        $pressure = Get-Content $pressureFile -Raw | ConvertFrom-Json
                        Write-Host "pressure: $($pressure.score) (target_pool=$($pressure.target_pool), updated=$($pressure.updated))"
                    } catch {
                        Get-Content $pressureFile
                    }
                }
                break
            }
            default {
                Write-Host "Usage: urnet-tools.ps1 self-heal <on|off|status>"
                break
            }
        }
        break
    }

    "hot-restart" {
        $mode = if ($SubArgs) { $SubArgs[0] } else { "" }
        $envVarName = "URNETWORK_HOT_RESTART"
        $restartNeeded = $false

        switch ($mode) {
            "on" {
                Write-Host "Enabling hot-restart (client JWT reuse across restarts)"
                [Environment]::SetEnvironmentVariable($envVarName, $null,
                    [System.EnvironmentVariableTarget]::User)
                Remove-Item Env:\URNETWORK_HOT_RESTART -ErrorAction SilentlyContinue
                Write-Host "URNETWORK_HOT_RESTART removed from user environment."
                $restartNeeded = $true
                break
            }
            "off" {
                Write-Host "Disabling hot-restart"
                [Environment]::SetEnvironmentVariable($envVarName, "0",
                    [System.EnvironmentVariableTarget]::User)
                $env:URNETWORK_HOT_RESTART = "0"
                Write-Host "URNETWORK_HOT_RESTART=0 set in user environment."
                $restartNeeded = $true
                break
            }
            "" {
                # Note: reads persistent registry state (User scope), not the
                # running provider's live environment — reports what the next
                # fresh process will see, not the current session.
                if (Test-HotRestartEnabled) {
                    Write-Host "Hot-restart is enabled."
                } else {
                    Write-Host "Hot-restart is off."
                }
                break
            }
            default {
                Write-Host "Usage: urnet-tools.ps1 hot-restart <on|off>"
                Write-Host "       urnet-tools.ps1 hot-restart          (show status)"
                break
            }
        }

        if ($restartNeeded -and -not ($mode -eq "")) {
            if (-not $NoConfirm) {
                $yn = Read-Host "Restart provider to apply? [y/N]"
                if ($yn -ne "y") {
                    Write-Host "Change applied. Run 'urnet-tools.ps1 restart' when ready."
                    break
                }
            }
            Write-Host "Restarting provider..."
            & $MyInvocation.MyCommand.Path stop -NoConfirm
            Start-Sleep -Seconds 2
            & $MyInvocation.MyCommand.Path start
            Write-Host "Provider restarted."
        }
        break
    }

    "proxy" {
        $useDocker = Test-ProviderIsDocker
        $proxySubCmd = if ($SubArgs) { $SubArgs[0] } else { "" }
        $rest = if ($SubArgs.Length -gt 1) { $SubArgs[1..($SubArgs.Length - 1)] } else { @() }
        switch ($proxySubCmd) {
            "add" {
                if ($useDocker) { docker exec -i $ContainerName provider proxy add @rest } else { & $BinaryPath proxy add @rest }
            }
            "clear" {
                if ($useDocker) { docker exec $ContainerName provider proxy remove --all } else { & $BinaryPath proxy remove --all }
            }
            "refresh" {
                if ($useDocker) { docker exec $ContainerName provider proxy refresh } else { & $BinaryPath proxy refresh }
            }
            "reload" {
                if ($useDocker) { docker exec $ContainerName provider proxy refresh } else { & $BinaryPath proxy refresh }
            }
            "remove-dead" {
                if ($useDocker) { docker exec $ContainerName provider proxy remove-dead @rest } else { & $BinaryPath proxy remove-dead @rest }
            }
            "summary" {
                if ($useDocker) { docker exec $ContainerName provider proxy summary } else { & $BinaryPath proxy summary }
            }
            "health" {
                if ($useDocker) {
                    docker exec $ContainerName proxy-health
                } else {
                    $healthDir = "$env:USERPROFILE\.urnetwork"
                    $stateFile = Join-Path $healthDir "proxy_health.state"
                    $logFile = Join-Path $healthDir "proxy_health.log"
                    if (Test-Path $stateFile) {
                        Write-Host "== Current proxy health ($stateFile) =="
                        Get-Content $stateFile
                    } else {
                        Write-Host "No snapshot yet at $stateFile (waiting for first heartbeat?)."
                    }
                    if (Test-Path $logFile) {
                        Write-Host ""
                        Write-Host "== Proxy health events ($logFile) -- Ctrl-C to stop =="
                        Get-Content $logFile -Tail 20 -Wait
                    } else {
                        Write-Host "No event log yet at $logFile."
                    }
                }
            }
            "traffic" {
                if ($useDocker) {
                    docker exec $ContainerName proxy-traffic
                } else {
                    $trafficFile = Join-Path "$env:USERPROFILE\.urnetwork" "proxy_traffic.state"
                    if (Test-Path $trafficFile) {
                        Get-Content $trafficFile
                    } else {
                        Write-Host "No traffic report yet at $trafficFile (waiting for first heartbeat?)."
                    }
                }
            }
            "remove" {
                if ($useDocker) { docker exec -i $ContainerName provider proxy remove @rest } else { & $BinaryPath proxy remove @rest }
            }
            "exclude" {
                if ($useDocker) { docker exec $ContainerName provider proxy exclude @rest } else { & $BinaryPath proxy exclude @rest }
            }
            default {
                Write-Host "Usage: proxy [add <file>|clear|refresh|reload|remove-dead|summary|health|traffic|remove --match=<pat>|exclude [<pat>] [--remove]]"
            }
        }
        break
    }

    "choose_network" {
        $useDocker = Test-ProviderIsDocker
        if ($SubArgs -and $SubArgs[0] -eq "--reset") {
            if ($useDocker) { docker exec $ContainerName provider choose_network --reset } else { & $BinaryPath choose_network --reset }
        }
        elseif ($SubArgs.Length -eq 2) {
            if ($useDocker) { docker exec $ContainerName provider choose_network $SubArgs[0] $SubArgs[1] } else { & $BinaryPath choose_network $SubArgs[0] $SubArgs[1] }
        }
        else {
            Write-Host "Usage: choose_network <api_url> <connect_url> | choose_network --reset"
        }
        break
    }

    "logs" {
        $useDocker = Test-ProviderIsDocker
        $n = "0"
        if ($SubArgs -contains "-n") {
            $nIndex = [array]::IndexOf($SubArgs, "-n")
            if ($nIndex -ge 0 -and $nIndex -lt ($SubArgs.Length - 1)) {
                $n = $SubArgs[$nIndex + 1]
            }
        }
        if ($useDocker) { docker exec $ContainerName provider logs -n $n } else { & $BinaryPath logs -n $n }
        break
    }

    default {
        Write-Error "Invalid command: $Command"
        exit 1
    }
}
