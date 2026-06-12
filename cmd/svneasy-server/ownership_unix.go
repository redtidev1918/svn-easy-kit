//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func copyRepositoryOwnership(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Chown(path, int(stat.Uid), int(stat.Gid))
	})
}

func readSecret(prompt string) (string, error) {
	print(prompt)
	stty, err := findExecutable("stty")
	if err != nil {
		var value string
		_, scanErr := fmt.Scanln(&value)
		return value, scanErr
	}
	command := exec.Command(stty, "-echo")
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		return "", err
	}
	defer func() {
		restore := exec.Command(stty, "echo")
		restore.Stdin = os.Stdin
		_ = restore.Run()
		println()
	}()
	reader := bufio.NewReader(os.Stdin)
	value, err := reader.ReadString('\n')
	return strings.TrimSpace(value), err
}
