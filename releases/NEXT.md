# Next release

This release covers changes merged after `v2026.11.6-1091259400-meso`. I focused on safer provider lifecycle operations and clearer operator guidance.

## Provider lifecycle and uninstall

- On Windows, `urnet-tools start` checks for a provider process or a responding control socket before launching another copy. If the process list cannot be read, it warns and checks the socket only.
- Windows graceful `stop` and `restart` refuse to act when another provider may share the profile or the process list cannot be checked. Forced `stop` bypasses the graceful-stop guard. A live socket is not removed during stale-socket cleanup.
- Windows update and tune restarts now use the Windows process route. A running provider can restart; a provider that was stopped stays stopped, and the change takes effect when it is started.
- `urnet-tools uninstall` stops the target before removing files. On Unix it checks the target PID and control socket after the stop. On Windows it also checks for other processes that may share the profile and rechecks after stopping. It aborts removal if it cannot verify the stop.
- The Windows PowerShell uninstaller checks for other provider processes, checks termination results and confirms that providers have exited. It refuses removal if a process path or query cannot be verified. Stop any extra provider first and retry if the guard blocks uninstall.

## Linux and Docker capability safety

The Linux installer, Docker update scripts and Go updater check the process capability bounding set before applying `cap_net_bind_service`. A blocked capability can prevent the binary from starting. The shell scripts remove an existing blocked bind capability only when that does not erase unrelated capabilities; they stop if they cannot remove it safely. A warning identifies a blocked capability. Check the host or container configuration if the extender needs a privileged port.

## Documentation

- The macOS installation examples now pass the required `install` argument.
- Docker guides show how to run `provider direct` and `provider sn-status` inside the container. The image's shell `urnet-tools` does not implement those commands.
- Node identity guidance now explains that a name saved through the control socket takes precedence over the legacy node-name file and environment names.

## Deploy notes

Update `urnet-tools` on provider hosts to get the lifecycle and uninstall changes. Use the updated installer or Docker scripts for their capability and uninstall safeguards. If a Windows command reports another provider that may share the profile, check the running processes and stop the extra one before retrying.

## What's Changed

- Prepare the previous release by @full-bars in #81
- Port provider lifecycle and installer safety fixes by @full-bars in #82
