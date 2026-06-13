package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const version = "1.1.0"

var repositoryNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type server struct {
	root     string
	svn      string
	svnadmin string
}

type createOptions struct {
	source       string
	name         string
	createLayout bool
	user         string
	password     string
	access       string
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("SvnEasyServer", version)
		return
	}
	if len(os.Args) == 1 {
		if err := interactiveMenu(); err != nil {
			exitError(err)
		}
		return
	}

	command := strings.ToLower(os.Args[1])
	args := os.Args[2:]
	var err error
	switch command {
	case "create":
		err = createCommand(args)
	case "user":
		err = userCommand(args)
	case "permission":
		err = permissionCommand(args)
	case "list":
		err = listCommand(args)
	case "doctor":
		err = doctorCommand(args)
	case "help", "-h", "--help":
		printHelp()
	default:
		err = fmt.Errorf("unknown command %q; run with help", command)
	}
	if err != nil {
		exitError(err)
	}
}

func printHelp() {
	fmt.Printf(`SvnEasyServer %s

Usage:
  SvnEasyServer                         Interactive menu
  SvnEasyServer list [--root DIR]
  SvnEasyServer doctor [--root DIR]
  SvnEasyServer create --from OLD --name NEW [options]
  SvnEasyServer user --repo REPO --name USER [options]
  SvnEasyServer permission --repo REPO --principal USER [options]

Create options:
  --root DIR          Repository root; auto-detected from svnserve -r
  --from NAME         Existing repository used as the permission template
  --name NAME         New repository name
  --layout            Create trunk, branches and tags (default true)
  --user NAME         Optionally add/update a user after creation
  --password VALUE    Password; omit to enter it interactively
  --access rw|r|none  Root access for the optional user (default rw)

Permission options:
  --path /            Repository path (default /)
  --access rw|r|none  Permission value
`, version)
}

func newServer(explicitRoot string) (*server, error) {
	root, err := detectRepositoryRoot(explicitRoot)
	if err != nil {
		return nil, err
	}
	svnadmin, err := findExecutable("svnadmin")
	if err != nil {
		return nil, errors.New("svnadmin was not found; install the Subversion server package")
	}
	svn, _ := findExecutable("svn")
	return &server{root: root, svn: svn, svnadmin: svnadmin}, nil
}

func createCommand(args []string) error {
	flags := flag.NewFlagSet("create", flag.ContinueOnError)
	root := flags.String("root", "", "repository root")
	source := flags.String("from", "", "source repository")
	name := flags.String("name", "", "new repository")
	layout := flags.Bool("layout", true, "create trunk/branches/tags")
	user := flags.String("user", "", "optional user")
	password := flags.String("password", "", "optional password")
	access := flags.String("access", "rw", "rw, r or none")
	if err := flags.Parse(args); err != nil {
		return err
	}
	s, err := newServer(*root)
	if err != nil {
		return err
	}
	return s.create(createOptions{
		source:       *source,
		name:         *name,
		createLayout: *layout,
		user:         *user,
		password:     *password,
		access:       *access,
	})
}

func userCommand(args []string) error {
	flags := flag.NewFlagSet("user", flag.ContinueOnError)
	root := flags.String("root", "", "repository root")
	repo := flags.String("repo", "", "repository")
	name := flags.String("name", "", "user name")
	password := flags.String("password", "", "password")
	access := flags.String("access", "rw", "root access")
	if err := flags.Parse(args); err != nil {
		return err
	}
	s, err := newServer(*root)
	if err != nil {
		return err
	}
	if *repo == "" || *name == "" {
		return errors.New("--repo and --name are required")
	}
	value := *password
	if value == "" {
		value, err = readSecret("Password: ")
		if err != nil {
			return err
		}
	}
	return s.addOrUpdateUser(*repo, *name, value, *access)
}

func permissionCommand(args []string) error {
	flags := flag.NewFlagSet("permission", flag.ContinueOnError)
	root := flags.String("root", "", "repository root")
	repo := flags.String("repo", "", "repository")
	principal := flags.String("principal", "", "user or @group")
	repoPath := flags.String("path", "/", "repository path")
	access := flags.String("access", "rw", "rw, r or none")
	if err := flags.Parse(args); err != nil {
		return err
	}
	s, err := newServer(*root)
	if err != nil {
		return err
	}
	if *repo == "" || *principal == "" {
		return errors.New("--repo and --principal are required")
	}
	return s.setPermission(*repo, *principal, *repoPath, *access)
}

func listCommand(args []string) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	root := flags.String("root", "", "repository root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	s, err := newServer(*root)
	if err != nil {
		return err
	}
	repositories, err := s.repositories()
	if err != nil {
		return err
	}
	fmt.Println("Repository root:", s.root)
	for _, repository := range repositories {
		fmt.Println(" -", repository)
	}
	return nil
}

func doctorCommand(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	root := flags.String("root", "", "repository root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	s, err := newServer(*root)
	if err != nil {
		return err
	}
	fmt.Println("SvnEasyServer:", version)
	fmt.Println("OS:", runtime.GOOS, runtime.GOARCH)
	fmt.Println("Repository root:", s.root)
	fmt.Println("svnadmin:", s.svnadmin)
	if s.svn == "" {
		fmt.Println("svn: missing (repository creation works; standard layout creation is unavailable)")
	} else {
		fmt.Println("svn:", s.svn)
	}
	repositories, err := s.repositories()
	if err != nil {
		return err
	}
	fmt.Printf("Repositories: %d\n", len(repositories))
	return nil
}

func (s *server) create(options createOptions) error {
	if !repositoryNamePattern.MatchString(options.source) {
		return errors.New("source repository name is required and may contain letters, digits, dot, underscore and hyphen")
	}
	if !repositoryNamePattern.MatchString(options.name) {
		return errors.New("new repository name is required and may contain letters, digits, dot, underscore and hyphen")
	}
	if options.source == options.name {
		return errors.New("source and new repository names must differ")
	}
	if _, err := normalizeAccess(options.access); err != nil {
		return err
	}

	sourcePath := filepath.Join(s.root, options.source)
	newPath := filepath.Join(s.root, options.name)
	if !isRepository(sourcePath) {
		return fmt.Errorf("source is not an SVN repository: %s", sourcePath)
	}
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("target already exists: %s", newPath)
	} else if !os.IsNotExist(err) {
		return err
	}

	fmt.Printf("Creating repository %s from permission template %s...\n", options.name, options.source)
	if _, err := runCommand(s.svnadmin, s.root, "create", newPath); err != nil {
		return err
	}
	if err := cloneConfiguration(sourcePath, newPath, options.source, options.name); err != nil {
		return fmt.Errorf("repository was created but configuration migration failed: %w", err)
	}

	if options.createLayout {
		if s.svn == "" {
			return errors.New("repository was created, but svn is missing so trunk/branches/tags could not be created")
		}
		if err := s.createStandardLayout(newPath); err != nil {
			return fmt.Errorf("repository was created, but standard layout creation failed: %w", err)
		}
	}

	if options.user != "" {
		password := options.password
		if password == "" {
			var err error
			password, err = readSecret("Password for " + options.user + ": ")
			if err != nil {
				return err
			}
		}
		if err := s.addOrUpdateUser(options.name, options.user, password, options.access); err != nil {
			return err
		}
	}
	if err := copyRepositoryOwnership(sourcePath, newPath); err != nil {
		return fmt.Errorf("repository was created but ownership migration failed: %w", err)
	}

	fmt.Println("Created:", newPath)
	fmt.Printf("URL: svn://SERVER/%s/trunk\n", options.name)
	fmt.Println("svnserve restart is normally not required.")
	return nil
}

func (s *server) createStandardLayout(repositoryPath string) error {
	base := repositoryFileURL(repositoryPath)
	_, err := runCommand(s.svn, s.root,
		"mkdir",
		base+"/trunk",
		base+"/branches",
		base+"/tags",
		"-m", "Initialize standard repository layout",
	)
	return err
}

func repositoryFileURL(path string) string {
	slashPath := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}

func (s *server) addOrUpdateUser(repository, user, password, access string) error {
	if !repositoryNamePattern.MatchString(repository) {
		return errors.New("invalid repository name")
	}
	if strings.TrimSpace(user) == "" || strings.ContainsAny(user, "=\r\n") {
		return errors.New("invalid user name")
	}
	if password == "" || strings.ContainsAny(password, "\r\n") {
		return errors.New("password cannot be empty or contain a newline")
	}
	normalizedAccess, err := normalizeAccess(access)
	if err != nil {
		return err
	}
	repositoryPath := filepath.Join(s.root, repository)
	if !isRepository(repositoryPath) {
		return fmt.Errorf("repository not found: %s", repository)
	}

	configPath := filepath.Join(repositoryPath, "conf", "svnserve.conf")
	config, err := readIniValues(configPath, "general")
	if err != nil {
		return err
	}
	passwordPath := resolveConfigFile(repositoryPath, config["password-db"], "passwd")
	if err := upsertIniKeyFile(passwordPath, "users", user, password); err != nil {
		return err
	}
	fmt.Println("Updated user database:", passwordPath)

	if err := s.setPermission(repository, user, "/", normalizedAccess); err != nil {
		return err
	}
	return nil
}

func (s *server) setPermission(repository, principal, repositorySubpath, access string) error {
	if !repositoryNamePattern.MatchString(repository) {
		return errors.New("invalid repository name")
	}
	if strings.TrimSpace(principal) == "" || strings.ContainsAny(principal, "=\r\n") {
		return errors.New("invalid principal")
	}
	normalizedAccess, err := normalizeAccess(access)
	if err != nil {
		return err
	}
	repositorySubpath = "/" + strings.TrimLeft(strings.TrimSpace(repositorySubpath), "/")
	repositoryPath := filepath.Join(s.root, repository)
	if !isRepository(repositoryPath) {
		return fmt.Errorf("repository not found: %s", repository)
	}

	configPath := filepath.Join(repositoryPath, "conf", "svnserve.conf")
	config, err := readIniValues(configPath, "general")
	if err != nil {
		return err
	}
	authzPath := resolveConfigFile(repositoryPath, config["authz-db"], "authz")
	data, err := os.ReadFile(authzPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	section := chooseAuthzSection(string(data), repository, repositorySubpath)
	value := normalizedAccess
	if value == "none" {
		value = ""
	}
	if err := upsertIniKeyFile(authzPath, section, principal, value); err != nil {
		return err
	}
	fmt.Printf("Permission: %s %s = %s (%s)\n", section, principal, normalizedAccess, authzPath)
	return nil
}

func normalizeAccess(access string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(access)) {
	case "rw", "wr":
		return "rw", nil
	case "r":
		return "r", nil
	case "", "none", "-":
		return "none", nil
	default:
		return "", errors.New("access must be rw, r or none")
	}
}

func chooseAuthzSection(content, repository, repositorySubpath string) string {
	repoPrefix := "[" + repository + ":"
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), repoPrefix) {
			return repository + ":" + repositorySubpath
		}
	}
	return repositorySubpath
}

func cloneConfiguration(sourceRepository, newRepository, oldName, newName string) error {
	sourceConf := filepath.Join(sourceRepository, "conf")
	newConf := filepath.Join(newRepository, "conf")
	if err := copyDirectory(sourceConf, newConf); err != nil {
		return err
	}

	configPath := filepath.Join(newConf, "svnserve.conf")
	values, err := readIniValues(configPath, "general")
	if err != nil {
		return err
	}
	files := []struct {
		key      string
		fallback string
	}{
		{"password-db", "passwd"},
		{"authz-db", "authz"},
		{"groups-db", "groups"},
	}
	for _, item := range files {
		value := strings.TrimSpace(values[item.key])
		if value == "" {
			continue
		}
		if strings.Contains(value, "://") {
			fmt.Printf("Warning: preserving external %s value %q\n", item.key, value)
			continue
		}
		sourceFile := value
		if !filepath.IsAbs(sourceFile) {
			sourceFile = filepath.Join(sourceConf, sourceFile)
		}
		if _, err := os.Stat(sourceFile); err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("Warning: referenced %s file does not exist: %s\n", item.key, sourceFile)
				continue
			}
			return err
		}
		targetName := item.fallback
		targetFile := filepath.Join(newConf, targetName)
		if err := copyFile(sourceFile, targetFile); err != nil {
			return err
		}
		if filepath.IsAbs(value) || filepath.Clean(value) != targetName {
			if err := upsertIniKeyFile(configPath, "general", item.key, targetName); err != nil {
				return err
			}
		}
	}

	authzPath := filepath.Join(newConf, "authz")
	if data, err := os.ReadFile(authzPath); err == nil {
		rewritten := rewriteAuthzRepository(string(data), oldName, newName)
		if err := atomicWrite(authzPath, []byte(rewritten), fileMode(authzPath)); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := upsertIniKeyFile(configPath, "general", "realm", newName); err != nil {
		return err
	}
	return nil
}

func rewriteAuthzRepository(content, oldName, newName string) string {
	lines := strings.SplitAfter(content, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		prefix := "[" + oldName + ":"
		if strings.HasPrefix(trimmed, prefix) && strings.HasSuffix(trimmed, "]") {
			replacement := "[" + newName + ":" + strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), "]") + "]"
			lines[index] = strings.Replace(line, trimmed, replacement, 1)
		}
	}
	return strings.Join(lines, "")
}

func resolveConfigFile(repositoryPath, configured, fallback string) string {
	value := strings.TrimSpace(configured)
	if value == "" || strings.Contains(value, "://") {
		value = fallback
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(repositoryPath, "conf", value)
}

func readIniValues(path, wantedSection string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string)
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
			continue
		}
		if section != wantedSection || trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values, nil
}

func upsertIniKeyFile(path, section, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	updated := upsertIniKey(string(data), section, key, value)
	mode := fs.FileMode(0o600)
	if err == nil {
		mode = fileMode(path)
		if backupErr := backupFile(path); backupErr != nil {
			return backupErr
		}
	}
	return atomicWrite(path, []byte(updated), mode)
}

func upsertIniKey(content, wantedSection, wantedKey, value string) string {
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
		content = strings.ReplaceAll(content, "\r\n", "\n")
	}
	hadFinalNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}

	sectionStart := -1
	sectionEnd := len(lines)
	keyIndex := -1
	currentSection := ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			nextSection := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
			if currentSection == wantedSection && sectionEnd == len(lines) {
				sectionEnd = index
			}
			currentSection = nextSection
			if currentSection == wantedSection && sectionStart < 0 {
				sectionStart = index
			}
			continue
		}
		if currentSection == wantedSection {
			candidate, _, ok := strings.Cut(trimmed, "=")
			if ok && strings.TrimSpace(candidate) == wantedKey {
				keyIndex = index
			}
		}
	}
	if sectionStart >= 0 && sectionEnd == len(lines) {
		sectionEnd = len(lines)
	}

	entry := wantedKey + " = " + value
	switch {
	case keyIndex >= 0:
		lines[keyIndex] = entry
	case sectionStart >= 0:
		lines = append(lines[:sectionEnd], append([]string{entry}, lines[sectionEnd:]...)...)
	default:
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "["+wantedSection+"]", entry)
	}
	result := strings.Join(lines, "\n")
	if hadFinalNewline || result != "" {
		result += "\n"
	}
	if newline == "\r\n" {
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}
	return result
}

func backupFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	backup := path + ".bak-" + time.Now().Format("20060102-150405.000")
	return copyFile(path, backup)
}

func atomicWrite(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".svneasy-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, mode); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	return os.Rename(tempPath, path)
}

func copyDirectory(source, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(destination, info.Mode().Perm())
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(destination)
			return os.Symlink(link, destination)
		}
		return copyFile(path, destination)
	})
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func fileMode(path string) fs.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0o600
}

func detectRepositoryRoot(explicit string) (string, error) {
	candidates := []string{explicit, os.Getenv("SVN_REPO_ROOT")}
	if runtime.GOOS == "linux" {
		if detected := detectLinuxSvnserveRoot(); detected != "" {
			candidates = append(candidates, detected)
		}
	}
	candidates = append(candidates,
		"/var/svn/repos",
		"/srv/svn",
		"/var/lib/svn",
		`C:\Repositories`,
		`C:\SVNRepositories`,
	)
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(absolute); err == nil && info.IsDir() {
			return filepath.Clean(absolute), nil
		}
	}
	return "", errors.New("cannot detect repository root; pass --root or set SVN_REPO_ROOT")
}

func detectLinuxSvnserveRoot() string {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		if len(args) == 0 || !strings.Contains(filepath.Base(args[0]), "svnserve") {
			continue
		}
		for index, arg := range args {
			switch {
			case (arg == "-r" || arg == "--root") && index+1 < len(args):
				return args[index+1]
			case strings.HasPrefix(arg, "--root="):
				return strings.TrimPrefix(arg, "--root=")
			case strings.HasPrefix(arg, "-r") && len(arg) > 2:
				return strings.TrimPrefix(arg, "-r")
			}
		}
	}
	return ""
}

func (s *server) repositories() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var repositories []string
	for _, entry := range entries {
		if entry.IsDir() && isRepository(filepath.Join(s.root, entry.Name())) {
			repositories = append(repositories, entry.Name())
		}
	}
	sort.Strings(repositories)
	return repositories, nil
}

func isRepository(path string) bool {
	required := []string{
		filepath.Join(path, "format"),
		filepath.Join(path, "db"),
		filepath.Join(path, "conf"),
	}
	for _, item := range required {
		if _, err := os.Stat(item); err != nil {
			return false
		}
	}
	return true
}

func findExecutable(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	candidates := []string{
		filepath.Join("/usr/bin", name),
		filepath.Join("/usr/local/bin", name),
		filepath.Join(os.Getenv("ProgramFiles"), "SlikSvn", "bin", name+".exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "TortoiseSVN", "bin", name+".exe"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found", name)
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

func interactiveMenu() error {
	s, err := newServer("")
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		repositories, err := s.repositories()
		if err != nil {
			return err
		}
		fmt.Printf("\nSVN Easy Server %s\n", version)
		fmt.Println("Repository folder:", s.root)
		fmt.Printf("Found %d repositories\n", len(repositories))
		fmt.Println("1. Create a new repository (recommended)")
		fmt.Println("2. Add or change a user")
		fmt.Println("3. Change access permission")
		fmt.Println("4. Show system information")
		fmt.Println("0. Exit")
		choice, err := prompt(reader, "Choose", "1")
		if err != nil {
			return err
		}
		switch choice {
		case "1":
			if len(repositories) == 0 {
				fmt.Println("No source repository found.")
				continue
			}
			source, selectErr := selectRepository(reader, "Copy users and permissions from", repositories)
			if selectErr != nil {
				return selectErr
			}
			name, _ := prompt(reader, "New repository name", "")
			options := createOptions{source: source, name: name, createLayout: true, access: "rw"}
			addUser, yesErr := promptYesNo(reader, "Add a new user now?", false)
			if yesErr != nil {
				return yesErr
			}
			if addUser {
				options.user, _ = prompt(reader, "User name", "")
				options.password, err = readSecret("Password for " + options.user + ": ")
				if err != nil {
					return err
				}
				options.access, err = selectAccess(reader)
				if err != nil {
					return err
				}
			}
			fmt.Println("\nReady to create:")
			fmt.Println("  New repository:", options.name)
			fmt.Println("  Copy settings from:", options.source)
			fmt.Println("  Create trunk/branches/tags: yes")
			if options.user != "" {
				fmt.Printf("  New user: %s (%s)\n", options.user, accessLabel(options.access))
			}
			confirmed, confirmErr := promptYesNo(reader, "Continue?", true)
			if confirmErr != nil {
				return confirmErr
			}
			if !confirmed {
				fmt.Println("Cancelled.")
				continue
			}
			if err := s.create(options); err != nil {
				fmt.Println("ERROR:", err)
			}
		case "2":
			if len(repositories) == 0 {
				fmt.Println("No repository found.")
				continue
			}
			repository, selectErr := selectRepository(reader, "Select repository", repositories)
			if selectErr != nil {
				return selectErr
			}
			user, _ := prompt(reader, "User name", "")
			password, passwordErr := readSecret("Password: ")
			if passwordErr != nil {
				return passwordErr
			}
			access, accessErr := selectAccess(reader)
			if accessErr != nil {
				return accessErr
			}
			if err := s.addOrUpdateUser(repository, user, password, access); err != nil {
				fmt.Println("ERROR:", err)
			}
		case "3":
			if len(repositories) == 0 {
				fmt.Println("No repository found.")
				continue
			}
			repository, selectErr := selectRepository(reader, "Select repository", repositories)
			if selectErr != nil {
				return selectErr
			}
			principal, _ := prompt(reader, "User or @group", "")
			path, _ := prompt(reader, "Repository path", "/")
			access, accessErr := selectAccess(reader)
			if accessErr != nil {
				return accessErr
			}
			if err := s.setPermission(repository, principal, path, access); err != nil {
				fmt.Println("ERROR:", err)
			}
		case "4":
			fmt.Println("svnadmin:", s.svnadmin)
			fmt.Println("svn:", valueOrMissing(s.svn))
			fmt.Println("Repository root:", s.root)
		case "0", "q", "quit", "exit":
			return nil
		default:
			fmt.Println("Unknown choice.")
		}
	}
}

func selectRepository(reader *bufio.Reader, title string, repositories []string) (string, error) {
	if len(repositories) == 1 {
		fmt.Printf("%s: %s\n", title, repositories[0])
		return repositories[0], nil
	}
	fmt.Println("\n" + title + ":")
	for index, repository := range repositories {
		fmt.Printf("%d. %s\n", index+1, repository)
	}
	for {
		value, err := prompt(reader, "Enter number", "1")
		if err != nil {
			return "", err
		}
		index, parseErr := strconv.Atoi(value)
		if parseErr == nil && index >= 1 && index <= len(repositories) {
			return repositories[index-1], nil
		}
		fmt.Println("Please enter a number shown above.")
	}
}

func selectAccess(reader *bufio.Reader) (string, error) {
	fmt.Println("1. Read and write (recommended)")
	fmt.Println("2. Read only")
	fmt.Println("3. No access")
	for {
		value, err := prompt(reader, "Choose permission", "1")
		if err != nil {
			return "", err
		}
		switch value {
		case "1":
			return "rw", nil
		case "2":
			return "r", nil
		case "3":
			return "none", nil
		default:
			fmt.Println("Please enter 1, 2 or 3.")
		}
	}
}

func accessLabel(access string) string {
	switch access {
	case "rw":
		return "read and write"
	case "r":
		return "read only"
	default:
		return "no access"
	}
}

func promptYesNo(reader *bufio.Reader, label string, defaultYes bool) (bool, error) {
	defaultValue := "n"
	if defaultYes {
		defaultValue = "Y"
	}
	value, err := prompt(reader, label+" (y/n)", defaultValue)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(value, "y") || strings.EqualFold(value, "yes"), nil
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

func valueOrMissing(value string) string {
	if value == "" {
		return "missing"
	}
	return value
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, "ERROR:", err)
	os.Exit(1)
}
