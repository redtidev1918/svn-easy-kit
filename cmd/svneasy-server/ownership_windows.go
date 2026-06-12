//go:build windows

package main

import (
	"bytes"
	"os/exec"
	"strings"
)

func copyRepositoryOwnership(source, target string) error {
	return nil
}

func readSecret(prompt string) (string, error) {
	script := `$p=Read-Host '` + strings.ReplaceAll(prompt, `'`, `''`) + `' -AsSecureString;` +
		`$b=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($p);` +
		`try{[Runtime.InteropServices.Marshal]::PtrToStringBSTR($b)}finally{[Runtime.InteropServices.Marshal]::ZeroFreeBSTR($b)}`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", script)
	cmd.Stdin = nil
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(output.String()), nil
}
