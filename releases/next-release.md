I added a setup command for the extender. It checks TCP 443, the provider binary's bind capability and the host firewall. It prints the exact commands and asks before it changes anything. In a non-interactive session, pass `--yes` or `--force` to approve changes. The setup preserves existing file capabilities when it adds `cap_net_bind_service`.

The setup plans firewalld runtime and permanent rules separately. It does not reload firewalld. UDP 53 is optional and user-managed. Setup does not open it.

FreeBSD extender setup is print-only and untested this release. It makes no changes. It prints the sysctl commands and the undo instructions. Regular providing on FreeBSD is unaffected.

Known limitations remain. The Docker log follower can replay retained lines. The hot-swap candidate can race to bind a socket that the current provider still holds.
