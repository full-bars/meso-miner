//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// restrictSocketACL sets a Windows DACL on the control socket file so
// that only the current user can connect. On Windows, os.Chmod 0600 is
// a no-op — without an explicit DACL every local user can reach the
// socket, which means any user on the machine could change provider
// settings.
func restrictSocketACL(path string) error {
	return restrictFileACL(path)
}

// restrictFileACL grants the current user FILE_GENERIC_READ | FILE_GENERIC_WRITE
// on path via a DACL. Everyone else is implicitly denied because the
// DACL contains only one ACE — the owner's.
func restrictFileACL(path string) error {
	// Current user SID from the process token.
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("open process token: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("get token user: %w", err)
	}
	sid := user.User.Sid

	// Single ACE: grant the current user read+write.
	entries := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		},
	}

	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return fmt.Errorf("build DACL: %w", err)
	}

	// Apply the DACL to the file. SetNamedSecurityInfo is the canonical
	// Win32 API for modifying a file's security descriptor; it works on
	// both regular files and named pipes (which is what Go's "unix"
	// domain sockets compile to on Windows).
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	); err != nil {
		return fmt.Errorf("set named security info: %w", err)
	}

	return nil
}
