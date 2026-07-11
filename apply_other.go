//go:build !darwin

package sparkle

import "fmt"

// InstalledAppBundle is macOS-only; returns "" elsewhere.
func InstalledAppBundle() string { return "" }

// Apply is macOS-only (the artifact is a macOS .app bundle).
func Apply(_ string) (string, error) {
	return "", fmt.Errorf("in-place update is macOS-only")
}

// ApplyArchiveTo is macOS-only.
func ApplyArchiveTo(_, _ string, _ bool) (string, error) {
	return "", fmt.Errorf("in-place update is macOS-only")
}

// CleanupBackups is a no-op off macOS.
func CleanupBackups(_ string) {}
