//go:build js

package connect

// js/wasm has no echo backend: every open fails with the unsupported
// sentinel, which classifies as a refusal (see ip_icmp_refusal.go).
func icmpEgressRefusalErrno(err error) bool { return false }

func icmpEgressUnusableErrno(err error) bool { return false }
