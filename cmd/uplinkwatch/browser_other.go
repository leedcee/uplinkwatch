//go:build !windows

package main

import "fmt"

func openBrowser(string) error {
	return fmt.Errorf("automatic browser opening is only supported on Windows")
}
