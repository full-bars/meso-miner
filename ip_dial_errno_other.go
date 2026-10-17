//go:build !windows

package connect

func isPlatformDialRefused(err error) bool { return false }

func isPlatformDialKernelTimeout(err error) bool { return false }
