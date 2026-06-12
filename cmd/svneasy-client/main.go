package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const version = "1.0.0"

type Config struct {
	WorkingCopy      string   `json:"workingCopy"`
	ScanRoot         string   `json:"scanRoot"`
	Targets          []string `json:"targets"`
	PollSeconds      int      `json:"pollSeconds"`
	RespectSvnIgnore bool     `json:"respectSvnIgnore"`
	AutoDelete       bool     `json:"autoDelete"`
	SvnExecutable    string   `json:"svnExecutable,omitempty"`
	LogFile          string   `json:"logFile,omitempty"`
}

type statusDocument struct {
	Targets []statusTarget `xml:"target"`
}

type statusTarget struct {
	Entries []statusEntry `xml:"entry"`
}

type statusEntry struct {
	Path   string   `xml:"path,attr"`
	Status wcStatus `xml:"wc-status"`
}

type wcStatus struct {
	Item string `xml:"item,attr"`
}

type tracker struct {
	config     Config
	configPath string
	svn        string
	targets    []string
	scanRoot   string
	log        io.Writer
	logCloser  io.Closer
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("SvnEasyClient", version)
		return
	}

	command := "watch"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = strings.ToLower(args[0])
		args = args[1:]
	}

	switch command {
	case "init":
		if err := initConfig(args); err != nil {
			exitError(err)
		}
	case "install":
		if err := installClient(args); err != nil {
			exitError(err)
		}
	case "uninstall":
		if err := uninstallClient(args); err != nil {
			exitError(err)
		}
	case "sync", "watch", "commit", "doctor":
		if err := runClientCommand(command, args); err != nil {
			exitError(err)
		}
	case "help", "-h", "--help":
		printHelp()
	default:
		exitError(fmt.Errorf("unknown command %q; run with help", command))
	}
}

func printHelp() {
	fmt.Printf(`SvnEasyClient %s

Usage:
  SvnEasyClient init [--config FILE]
  SvnEasyClient sync [--config FILE]
  SvnEasyClient watch [--config FILE]
  SvnEasyClient commit [--config FILE]
  SvnEasyClient doctor [--config FILE]
  SvnEasyClient install [--config FILE]
  SvnEasyClient uninstall [--config FILE]

Commands:
  init       Interactive configuration wizard
  sync       Register new and deleted whitelist files once
  watch      Watch whitelist paths and synchronize continuously
  commit     Synchronize, then open the TortoiseSVN commit dialog
  doctor     Check configuration and required SVN tools
  install    Install missing SVN CLI and enable background startup
  uninstall  Remove background startup; files and SVN data are untouched
`, version)
}

func runClientCommand(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	configPath := flags.String("config", defaultConfigPath(), "configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}

	t, err := newTracker(*configPath, false)
	if err != nil {
		return err
	}
	defer t.close()

	switch command {
	case "sync":
		return t.sync()
	case "watch":
		return t.watch()
	case "commit":
		if err := t.sync(); err != nil {
			return err
		}
		return t.openCommit()
	case "doctor":
		return t.doctor()
	}
	return nil
}

func newTracker(configPath string, autoInstall bool) (*tracker, error) {
	absoluteConfig, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(absoluteConfig)
	if err != nil {
		return nil, err
	}
	if err := normalizeConfig(&cfg, filepath.Dir(absoluteConfig)); err != nil {
		return nil, err
	}

	svn, err := findSvn(cfg.SvnExecutable)
	if err != nil && autoInstall {
		if installErr := installSvnDependency(); installErr != nil {
			return nil, fmt.Errorf("%v; automatic install also failed: %w", err, installErr)
		}
		svn, err = findSvn(cfg.SvnExecutable)
	}
	if err != nil {
		return nil, err
	}

	targets := make([]string, 0, len(cfg.Targets))
	for _, target := range cfg.Targets {
		resolved := target
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(cfg.WorkingCopy, resolved)
		}
		targets = append(targets, filepath.Clean(resolved))
	}

	scanRoot := cfg.ScanRoot
	if !filepath.IsAbs(scanRoot) {
		scanRoot = filepath.Join(cfg.WorkingCopy, scanRoot)
	}

	logWriter := io.Writer(os.Stdout)
	var closer io.Closer
	if cfg.LogFile != "" {
		logPath := cfg.LogFile
		if !filepath.IsAbs(logPath) {
			logPath = filepath.Join(filepath.Dir(absoluteConfig), logPath)
		}
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return nil, err
		}
		file, openErr := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if openErr != nil {
			return nil, openErr
		}
		logWriter = io.MultiWriter(os.Stdout, file)
		closer = file
	}

	return &tracker{
		config:     cfg,
		configPath: absoluteConfig,
		svn:        svn,
		targets:    targets,
		scanRoot:   filepath.Clean(scanRoot),
		log:        logWriter,
		logCloser:  closer,
	}, nil
}

func (t *tracker) close() {
	if t.logCloser != nil {
		_ = t.logCloser.Close()
	}
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("cannot read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

func normalizeConfig(cfg *Config, configDir string) error {
	if strings.TrimSpace(cfg.WorkingCopy) == "" {
		return errors.New("workingCopy is required")
	}
	if !filepath.IsAbs(cfg.WorkingCopy) {
		cfg.WorkingCopy = filepath.Join(configDir, cfg.WorkingCopy)
	}
	cfg.WorkingCopy = filepath.Clean(cfg.WorkingCopy)
	if stat, err := os.Stat(cfg.WorkingCopy); err != nil || !stat.IsDir() {
		return fmt.Errorf("working copy does not exist: %s", cfg.WorkingCopy)
	}
	if _, err := os.Stat(filepath.Join(cfg.WorkingCopy, ".svn")); err != nil {
		return fmt.Errorf("%s is not an SVN working-copy root (.svn not found)", cfg.WorkingCopy)
	}
	if len(cfg.Targets) == 0 {
		return errors.New("at least one whitelist target is required")
	}
	if cfg.ScanRoot == "" {
		cfg.ScanRoot = "."
	}
	if cfg.PollSeconds < 1 {
		cfg.PollSeconds = 2
	}

	rootWithSep := filepath.Clean(cfg.WorkingCopy) + string(os.PathSeparator)
	for _, target := range cfg.Targets {
		full := target
		if !filepath.IsAbs(full) {
			full = filepath.Join(cfg.WorkingCopy, full)
		}
		full = filepath.Clean(full)
		if !samePath(full, cfg.WorkingCopy) && !pathHasPrefix(full, rootWithSep) {
			return fmt.Errorf("target escapes the working copy: %s", target)
		}
	}
	return nil
}

func (t *tracker) sync() error {
	entries, err := t.status()
	if err != nil {
		return err
	}

	var additions, deletions []string
	modified := 0
	for _, entry := range entries {
		path := t.absoluteStatusPath(entry.Path)
		if !t.isWhitelisted(path) {
			continue
		}
		switch entry.Status.Item {
		case "unversioned":
			additions = append(additions, path)
		case "ignored":
			if !t.config.RespectSvnIgnore {
				additions = append(additions, path)
			}
		case "missing":
			if t.config.AutoDelete {
				deletions = append(deletions, path)
			}
		case "modified", "conflicted", "replaced", "added", "deleted":
			modified++
		}
	}

	additions = minimizePaths(additions)
	deletions = minimizePaths(deletions)

	for _, path := range additions {
		args := []string{"add", "--force", "--parents", "--depth", "infinity"}
		if !t.config.RespectSvnIgnore {
			args = append(args, "--no-ignore")
		}
		args = append(args, "--", path)
		if _, err := runCommand(t.svn, t.config.WorkingCopy, args...); err != nil {
			return err
		}
		t.logf("ADD    %s", path)
	}

	for _, path := range deletions {
		if _, err := runCommand(t.svn, t.config.WorkingCopy, "delete", "--force", "--", path); err != nil {
			return err
		}
		t.logf("DELETE %s", path)
	}

	if len(additions) == 0 && len(deletions) == 0 {
		t.logf("OK     no new or missing whitelist files (%d existing changes)", modified)
	} else {
		t.logf("SYNC   added=%d deleted=%d existing-changes=%d", len(additions), len(deletions), modified)
	}
	return nil
}

func (t *tracker) status() ([]statusEntry, error) {
	args := []string{"status", "--xml", "--depth", "infinity"}
	if !t.config.RespectSvnIgnore {
		args = append(args, "--no-ignore")
	}
	args = append(args, "--", t.scanRoot)
	output, err := runCommand(t.svn, t.config.WorkingCopy, args...)
	if err != nil {
		return nil, err
	}
	var document statusDocument
	if err := xml.Unmarshal([]byte(output), &document); err != nil {
		return nil, fmt.Errorf("cannot parse svn status XML: %w", err)
	}
	var entries []statusEntry
	for _, target := range document.Targets {
		entries = append(entries, target.Entries...)
	}
	return entries, nil
}

func (t *tracker) watch() error {
	t.logf("WATCH  SvnEasyClient %s", version)
	t.logf("WATCH  working copy: %s", t.config.WorkingCopy)
	for _, target := range t.targets {
		t.logf("WATCH  whitelist: %s", target)
	}
	if err := t.sync(); err != nil {
		return err
	}

	last, err := t.fingerprint()
	if err != nil {
		return err
	}
	ticker := time.NewTicker(time.Duration(t.config.PollSeconds) * time.Second)
	defer ticker.Stop()
	fullReconcile := 0

	for range ticker.C {
		current, err := t.fingerprint()
		if err != nil {
			t.logf("ERROR  %v", err)
			continue
		}
		fullReconcile++
		if current == last && fullReconcile < 30 {
			continue
		}
		last = current
		fullReconcile = 0
		if err := t.sync(); err != nil {
			t.logf("ERROR  %v", err)
		}
	}
	return nil
}

func (t *tracker) fingerprint() (string, error) {
	hash := sha256.New()
	for _, target := range t.targets {
		_, _ = io.WriteString(hash, target)
		err := filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					_, _ = io.WriteString(hash, "|missing|"+path)
					return nil
				}
				return walkErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			_, _ = io.WriteString(hash, path)
			_, _ = io.WriteString(hash, strconv.FormatInt(info.Size(), 10))
			_, _ = io.WriteString(hash, strconv.FormatInt(info.ModTime().UnixNano(), 10))
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (t *tracker) doctor() error {
	fmt.Fprintf(t.log, "SvnEasyClient %s\n", version)
	fmt.Fprintf(t.log, "Config:       %s\n", t.configPath)
	fmt.Fprintf(t.log, "Working copy: %s\n", t.config.WorkingCopy)
	fmt.Fprintf(t.log, "Scan root:    %s\n", t.scanRoot)
	fmt.Fprintf(t.log, "SVN:          %s\n", t.svn)
	versionOutput, err := runCommand(t.svn, t.config.WorkingCopy, "--version", "--quiet")
	if err != nil {
		return err
	}
	fmt.Fprintf(t.log, "SVN version:  %s\n", strings.TrimSpace(versionOutput))
	for _, target := range t.targets {
		if _, err := os.Stat(target); err == nil {
			fmt.Fprintf(t.log, "Target OK:    %s\n", target)
		} else if os.IsNotExist(err) {
			fmt.Fprintf(t.log, "Target absent (will track deletion): %s\n", target)
		} else {
			return err
		}
	}
	_, err = t.status()
	if err == nil {
		fmt.Fprintln(t.log, "Status check: OK")
	}
	return err
}

func (t *tracker) openCommit() error {
	commitPath := t.scanRoot
	if runtime.GOOS == "windows" {
		candidates := []string{
			filepath.Join(os.Getenv("ProgramFiles"), "TortoiseSVN", "bin", "TortoiseProc.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "TortoiseSVN", "bin", "TortoiseProc.exe"),
		}
		for _, candidate := range candidates {
			if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
				cmd := exec.Command(candidate, "/command:commit", "/path:"+commitPath)
				if err := cmd.Start(); err != nil {
					return err
				}
				t.logf("COMMIT opened TortoiseSVN for %s", commitPath)
				return nil
			}
		}
	}
	output, err := runCommand(t.svn, t.config.WorkingCopy, "status", "--", commitPath)
	if output != "" {
		fmt.Fprint(t.log, output)
	}
	if err != nil {
		return err
	}
	return errors.New("TortoiseSVN was not found; review the status above and run svn commit manually")
}

func (t *tracker) absoluteStatusPath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(t.config.WorkingCopy, path))
}

func (t *tracker) isWhitelisted(path string) bool {
	for _, target := range t.targets {
		if samePath(path, target) || pathHasPrefix(path, target+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func (t *tracker) logf(format string, values ...any) {
	fmt.Fprintf(t.log, "%s "+format+"\n", append([]any{time.Now().Format("2006-01-02 15:04:05")}, values...)...)
}

func findSvn(configured string) (string, error) {
	var candidates []string
	if configured != "" {
		candidates = append(candidates, configured)
	}
	if path, err := exec.LookPath("svn"); err == nil {
		candidates = append(candidates, path)
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			filepath.Join(os.Getenv("ProgramFiles"), "SlikSvn", "bin", "svn.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "TortoiseSVN", "bin", "svn.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "SlikSvn", "bin", "svn.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "TortoiseSVN", "bin", "svn.exe"),
		)
	} else {
		candidates = append(candidates, "/usr/bin/svn", "/usr/local/bin/svn", "/opt/homebrew/bin/svn")
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("svn command-line client was not found; run SvnEasyClient install to install it automatically")
}

func runCommand(executable, dir string, args ...string) (string, error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		return stdout.String(), fmt.Errorf("%s %s failed: %v: %s", executable, strings.Join(args, " "), err, message)
	}
	return stdout.String(), nil
}

func minimizePaths(paths []string) []string {
	unique := make(map[string]string)
	for _, path := range paths {
		clean := filepath.Clean(path)
		key := clean
		if runtime.GOOS == "windows" {
			key = strings.ToLower(clean)
		}
		unique[key] = clean
	}
	result := make([]string, 0, len(unique))
	for _, path := range unique {
		result = append(result, path)
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i]) == len(result[j]) {
			return result[i] < result[j]
		}
		return len(result[i]) < len(result[j])
	})
	var minimal []string
	for _, path := range result {
		covered := false
		for _, parent := range minimal {
			if samePath(path, parent) || pathHasPrefix(path, parent+string(os.PathSeparator)) {
				covered = true
				break
			}
		}
		if !covered {
			minimal = append(minimal, path)
		}
	}
	return minimal
}

func samePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func pathHasPrefix(path, prefix string) bool {
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix))
	}
	return strings.HasPrefix(path, prefix)
}

func defaultConfigPath() string {
	executable, err := os.Executable()
	if err != nil {
		return "svneasy-client.json"
	}
	return filepath.Join(filepath.Dir(executable), "svneasy-client.json")
}

func initConfig(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfigPath(), "configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)
	workingCopy, err := prompt(reader, "SVN working-copy root", "")
	if err != nil {
		return err
	}
	scanRoot, err := prompt(reader, "Project/scan path relative to working copy", ".")
	if err != nil {
		return err
	}
	targetText, err := prompt(reader, "Whitelist paths, comma-separated", scanRoot)
	if err != nil {
		return err
	}
	var targets []string
	for _, item := range strings.Split(targetText, ",") {
		if value := strings.TrimSpace(item); value != "" {
			targets = append(targets, value)
		}
	}
	cfg := Config{
		WorkingCopy:      workingCopy,
		ScanRoot:         scanRoot,
		Targets:          targets,
		PollSeconds:      2,
		RespectSvnIgnore: false,
		AutoDelete:       true,
		LogFile:          "svneasy-client.log",
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*configPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*configPath, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println("Created", *configPath)
	return nil
}

func prompt(reader *bufio.Reader, label, defaultValue string) (string, error) {
	if defaultValue == "" {
		fmt.Printf("%s: ", label)
	} else {
		fmt.Printf("%s [%s]: ", label, defaultValue)
	}
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultValue
	}
	return value, nil
}

func installClient(args []string) error {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfigPath(), "configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	t, err := newTracker(*configPath, true)
	if err != nil {
		return err
	}
	defer t.close()
	if err := t.doctor(); err != nil {
		return err
	}
	if err := t.sync(); err != nil {
		return err
	}
	if err := installAutostart(t.configPath); err != nil {
		return err
	}
	fmt.Println("Installation complete. Whitelist tracking will start automatically after login.")
	return nil
}

func uninstallClient(args []string) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	_ = flags.String("config", defaultConfigPath(), "configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return uninstallAutostart()
}

func installSvnDependency() error {
	fmt.Println("SVN command-line client is missing; installing it automatically...")
	switch runtime.GOOS {
	case "windows":
		winget, err := exec.LookPath("winget")
		if err != nil {
			return errors.New("winget is unavailable; install SlikSVN or the TortoiseSVN command-line tools")
		}
		_, err = runCommand(winget, ".", "install", "--id", "Slik.Subversion", "-e", "--silent",
			"--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity")
		return err
	case "linux":
		return installLinuxSvn()
	case "darwin":
		brew, err := exec.LookPath("brew")
		if err != nil {
			return errors.New("Homebrew is unavailable; install the Subversion command-line client")
		}
		_, err = runCommand(brew, ".", "install", "subversion")
		return err
	default:
		return fmt.Errorf("automatic SVN installation is not supported on %s", runtime.GOOS)
	}
}

func installLinuxSvn() error {
	type packageManager struct {
		name string
		args []string
	}
	managers := []packageManager{
		{"apt-get", []string{"install", "-y", "subversion"}},
		{"dnf", []string{"install", "-y", "subversion"}},
		{"yum", []string{"install", "-y", "subversion"}},
		{"zypper", []string{"--non-interactive", "install", "subversion"}},
		{"apk", []string{"add", "subversion"}},
		{"pacman", []string{"-S", "--noconfirm", "subversion"}},
	}
	for _, manager := range managers {
		path, err := exec.LookPath(manager.name)
		if err != nil {
			continue
		}
		args := manager.args
		executable := path
		if !runningAsRoot() {
			sudo, err := exec.LookPath("sudo")
			if err != nil {
				return fmt.Errorf("%s requires root; sudo was not found", manager.name)
			}
			args = append([]string{path}, args...)
			executable = sudo
		}
		_, err = runCommand(executable, ".", args...)
		return err
	}
	return errors.New("no supported package manager found")
}

func installAutostart(configPath string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, _ = filepath.Abs(executable)
	configPath, _ = filepath.Abs(configPath)

	switch runtime.GOOS {
	case "windows":
		startup := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
		if err := os.MkdirAll(startup, 0o755); err != nil {
			return err
		}
		script := filepath.Join(startup, "SvnEasyClient.vbs")
		command := fmt.Sprintf("\"%s\" watch --config \"%s\"", executable, configPath)
		content := "Set shell = CreateObject(\"WScript.Shell\")\r\n" +
			"shell.Run " + vbString(command) + ", 0, False\r\n"
		if err := os.WriteFile(script, []byte(content), 0o644); err != nil {
			return err
		}
		_ = exec.Command("wscript.exe", script).Start()
		fmt.Println("Installed background startup:", script)
		return nil
	case "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		unitDir := filepath.Join(home, ".config", "systemd", "user")
		if err := os.MkdirAll(unitDir, 0o755); err != nil {
			return err
		}
		unitPath := filepath.Join(unitDir, "svneasy-client.service")
		unit := fmt.Sprintf(`[Unit]
Description=SvnEasy whitelist tracker

[Service]
ExecStart=%s watch --config %s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, systemdQuote(executable), systemdQuote(configPath))
		if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
			return err
		}
		if _, err := runCommand("systemctl", ".", "--user", "daemon-reload"); err != nil {
			return err
		}
		_, err = runCommand("systemctl", ".", "--user", "enable", "--now", "svneasy-client.service")
		return err
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		agentDir := filepath.Join(home, "Library", "LaunchAgents")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			return err
		}
		agentPath := filepath.Join(agentDir, "com.svneasy.client.plist")
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.svneasy.client</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string><string>watch</string><string>--config</string><string>%s</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
`, xmlText(executable), xmlText(configPath))
		if err := os.WriteFile(agentPath, []byte(plist), 0o644); err != nil {
			return err
		}
		_, _ = runCommand("launchctl", ".", "unload", agentPath)
		_, err = runCommand("launchctl", ".", "load", agentPath)
		return err
	default:
		return fmt.Errorf("automatic startup is not supported on %s", runtime.GOOS)
	}
}

func uninstallAutostart() error {
	switch runtime.GOOS {
	case "windows":
		path := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "SvnEasyClient.vbs")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Println("Removed background startup:", path)
		return nil
	case "linux":
		_, _ = runCommand("systemctl", ".", "--user", "disable", "--now", "svneasy-client.service")
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path := filepath.Join(home, ".config", "systemd", "user", "svneasy-client.service")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		_, _ = runCommand("systemctl", ".", "--user", "daemon-reload")
		fmt.Println("Removed background startup:", path)
		return nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path := filepath.Join(home, "Library", "LaunchAgents", "com.svneasy.client.plist")
		_, _ = runCommand("launchctl", ".", "unload", path)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Println("Removed background startup:", path)
		return nil
	default:
		return fmt.Errorf("automatic startup is not supported on %s", runtime.GOOS)
	}
}

func vbString(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func systemdQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `\`, `\\`) + `"`
}

func xmlText(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, "ERROR:", err)
	os.Exit(1)
}
