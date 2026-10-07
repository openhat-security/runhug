//go:build !unix

package cli

func setStdinNonblock(bool) error { return nil }
