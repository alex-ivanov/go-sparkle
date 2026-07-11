//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// keychainKey reads the base64 EdDSA private key Sparkle's generate_keys stored
// in the login Keychain (generic password, service https://sparkle-project.org,
// the given account). Uses the `security` CLI so no cgo is needed.
func keychainKey(account string) (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-s", keychainService, "-a", account, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("not found in the login Keychain for account %q", account)
	}
	key := strings.TrimSpace(string(out))
	if key == "" {
		return "", fmt.Errorf("empty key in the login Keychain for account %q", account)
	}
	return key, nil
}
