package main

import "strings"

// sanitizeCandidateArgs strips identity-mutating arguments from the parent's
// argv so the HotSwap candidate never re-authenticates or re-executes
// auth-provide with a stale auth code (F-3). Returns the cleaned argument list.
func sanitizeCandidateArgs(args []string) []string {
	var cleanArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "auth-provide" {
			cleanArgs = append(cleanArgs, "provide")
			// If followed by positional auth code, skip it
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		if arg == "-f" {
			continue
		}
		if arg == "--user_auth" {
			// Skip the flag and its positional value (separated form: --user_auth <val>)
			i++
			continue
		}
		if arg == "--password" {
			// Skip the flag and its positional value (separated form: --password <val>)
			i++
			continue
		}
		if strings.HasPrefix(arg, "--user_auth=") || strings.HasPrefix(arg, "--password=") {
			continue
		}
		cleanArgs = append(cleanArgs, arg)
	}
	return cleanArgs
}
