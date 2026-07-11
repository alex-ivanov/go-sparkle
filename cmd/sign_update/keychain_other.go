//go:build !darwin

package main

import "fmt"

// keychainKey is macOS-only; elsewhere the key must be given via --ed-key-file.
func keychainKey(_ string) (string, error) {
	return "", fmt.Errorf("the Keychain is macOS-only")
}
