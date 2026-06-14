package main

import (
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMinimizePaths(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "wc", "Source")
	input := []string{
		filepath.Join(root, "Game"),
		filepath.Join(root, "Game", "Player.cpp"),
		filepath.Join(root, "Other.cpp"),
		filepath.Join(root, "Other.cpp"),
	}
	expected := []string{
		filepath.Join(root, "Game"),
		filepath.Join(root, "Other.cpp"),
	}
	actual := minimizePaths(input)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("minimizePaths() = %#v, want %#v", actual, expected)
	}
}

func TestWhitelistDoesNotIncludeSibling(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "wc")
	tracker := tracker{
		targets: []string{
			filepath.Join(root, "Project", "Source"),
			filepath.Join(root, "Project", "Game.uproject"),
		},
	}
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(root, "Project", "Source", "Player.cpp"), true},
		{filepath.Join(root, "Project", "Game.uproject"), true},
		{filepath.Join(root, "Project", "SourceBackup", "Player.cpp"), false},
		{filepath.Join(root, "Project", "Saved", "log.txt"), false},
	}
	for _, test := range cases {
		if actual := tracker.isWhitelisted(test.path); actual != test.want {
			t.Errorf("isWhitelisted(%q) = %v, want %v", test.path, actual, test.want)
		}
	}
}

func TestUnrealRecommendationsUseMinimalShareableSet(t *testing.T) {
	workingCopy := t.TempDir()
	if err := os.Mkdir(filepath.Join(workingCopy, ".svn"), 0o755); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(workingCopy, "ExampleGame")
	mustWriteTestFile(t, filepath.Join(project, "ExampleGame.uproject"), "{}")
	for _, directory := range []string{"Source", "Config", "Content", "Saved"} {
		if err := os.MkdirAll(filepath.Join(project, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteTestFile(t, filepath.Join(project, "ExampleGame.sln"), "generated")
	mustWriteTestFile(t, filepath.Join(project, ".vsconfig"), "generated")

	targets := suggestTargets(workingCopy, project)
	joined := strings.Join(targets, "|")
	for _, expected := range []string{"ExampleGame.uproject", "Source", "Config", "Content"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("recommended targets do not contain %q: %#v", expected, targets)
		}
	}
	if strings.Contains(joined, "Saved") {
		t.Fatalf("cache/output directory was recommended: %#v", targets)
	}
	if strings.Contains(joined, ".sln") || strings.Contains(joined, ".vsconfig") {
		t.Fatalf("generated IDE files were recommended: %#v", targets)
	}
}

func TestUnrealIgnorePatterns(t *testing.T) {
	project := t.TempDir()
	mustWriteTestFile(t, filepath.Join(project, "ExampleGame.uproject"), "{}")
	patterns := strings.Join(ignorePatterns(project), "|")
	for _, expected := range []string{
		"Binaries", "DerivedDataCache", "Intermediate", "Saved", "*.sln", ".vsconfig",
	} {
		if !strings.Contains(patterns, expected) {
			t.Fatalf("ignore patterns do not contain %q: %s", expected, patterns)
		}
	}
}

func TestIntegrationSyncWhitelist(t *testing.T) {
	if os.Getenv("SVNEASY_INTEGRATION") != "1" {
		t.Skip("set SVNEASY_INTEGRATION=1 to run")
	}
	svn, err := exec.LookPath("svn")
	if err != nil {
		svn = filepath.Join(os.Getenv("ProgramFiles"), "SlikSvn", "bin", "svn.exe")
	}
	svnadmin, err := exec.LookPath("svnadmin")
	if err != nil {
		svnadmin = filepath.Join(os.Getenv("ProgramFiles"), "SlikSvn", "bin", "svnadmin.exe")
	}
	if _, err := os.Stat(svn); err != nil {
		t.Skip("svn not installed")
	}
	if _, err := os.Stat(svnadmin); err != nil {
		t.Skip("svnadmin not installed")
	}

	root := t.TempDir()
	repository := filepath.Join(root, "repo")
	workingCopy := filepath.Join(root, "wc")
	runTestCommand(t, svnadmin, root, "create", repository)
	repositoryURL := testRepositoryFileURL(repository)
	runTestCommand(t, svn, root, "mkdir", repositoryURL+"/trunk", "-m", "init")
	runTestCommand(t, svn, root, "checkout", repositoryURL+"/trunk", workingCopy)

	project := filepath.Join(workingCopy, "Project")
	mustWriteTestFile(t, filepath.Join(project, "Source", "Existing.cpp"), "old")
	mustWriteTestFile(t, filepath.Join(project, "Source", "DeleteMe.cpp"), "delete")
	mustWriteTestFile(t, filepath.Join(project, "Project.uproject"), "{}")
	runTestCommand(t, svn, workingCopy, "add", project)
	runTestCommand(t, svn, workingCopy, "commit", "-m", "base")

	mustWriteTestFile(t, filepath.Join(project, "Source", "Existing.cpp"), "modified")
	mustWriteTestFile(t, filepath.Join(project, "Source", "New.cpp"), "new")
	if err := os.Remove(filepath.Join(project, "Source", "DeleteMe.cpp")); err != nil {
		t.Fatal(err)
	}
	mustWriteTestFile(t, filepath.Join(project, "Saved", "outside.txt"), "outside")

	tracker := tracker{
		config: Config{
			WorkingCopy:      workingCopy,
			RespectSvnIgnore: false,
			AutoDelete:       true,
		},
		svn:      svn,
		targets:  []string{filepath.Join(project, "Source"), filepath.Join(project, "Project.uproject")},
		scanRoot: project,
		log:      io.Discard,
	}
	if err := tracker.sync(); err != nil {
		t.Fatal(err)
	}

	status := runTestCommand(t, svn, workingCopy, "status")
	for _, expected := range []string{
		"A       Project" + string(filepath.Separator) + "Source" + string(filepath.Separator) + "New.cpp",
		"D       Project" + string(filepath.Separator) + "Source" + string(filepath.Separator) + "DeleteMe.cpp",
		"M       Project" + string(filepath.Separator) + "Source" + string(filepath.Separator) + "Existing.cpp",
		"?       Project" + string(filepath.Separator) + "Saved",
	} {
		if !strings.Contains(status, expected) {
			t.Fatalf("status does not contain %q:\n%s", expected, status)
		}
	}
}

func TestIntegrationFirstUploadKeepsUnrealGeneratedFilesOut(t *testing.T) {
	if os.Getenv("SVNEASY_INTEGRATION") != "1" {
		t.Skip("set SVNEASY_INTEGRATION=1 to run")
	}
	svn, err := exec.LookPath("svn")
	if err != nil {
		svn = filepath.Join(os.Getenv("ProgramFiles"), "SlikSvn", "bin", "svn.exe")
	}
	svnadmin, err := exec.LookPath("svnadmin")
	if err != nil {
		svnadmin = filepath.Join(os.Getenv("ProgramFiles"), "SlikSvn", "bin", "svnadmin.exe")
	}
	if _, err := os.Stat(svn); err != nil {
		t.Skip("svn not installed")
	}
	if _, err := os.Stat(svnadmin); err != nil {
		t.Skip("svnadmin not installed")
	}

	root := t.TempDir()
	repository := filepath.Join(root, "repo")
	project := filepath.Join(root, "FlappyBird")
	runTestCommand(t, svnadmin, root, "create", repository)
	repositoryURL := testRepositoryFileURL(repository)
	runTestCommand(t, svn, root, "mkdir", repositoryURL+"/trunk", "-m", "init")

	mustWriteTestFile(t, filepath.Join(project, "Config", "DefaultGame.ini"), "[game]")
	mustWriteTestFile(t, filepath.Join(project, "Content", "Bird.uasset"), "asset")
	mustWriteTestFile(t, filepath.Join(project, "Source", "Bird.cpp"), "code")
	mustWriteTestFile(t, filepath.Join(project, "FlappyBird.uproject"), "{}")
	mustWriteTestFile(t, filepath.Join(project, "Intermediate", "huge.tmp"), "generated")
	mustWriteTestFile(t, filepath.Join(project, "Binaries", "Game.dll"), "generated")
	mustWriteTestFile(t, filepath.Join(project, "Saved", "log.txt"), "generated")
	mustWriteTestFile(t, filepath.Join(project, "FlappyBird.sln"), "generated")
	mustWriteTestFile(t, filepath.Join(project, ".vsconfig"), "generated")

	runTestCommand(t, svn, root, "checkout", "--depth", "empty", repositoryURL+"/trunk", project)
	if err := applyIgnoreProperty(svn, project, ignorePatterns(project)); err != nil {
		t.Fatal(err)
	}
	targets := suggestTargets(project, project)
	tracker := tracker{
		config: Config{
			WorkingCopy:      project,
			RespectSvnIgnore: false,
			AutoDelete:       true,
		},
		svn:      svn,
		targets:  absoluteTargets(project, targets),
		scanRoot: project,
		log:      io.Discard,
	}
	if err := tracker.sync(); err != nil {
		t.Fatal(err)
	}

	status := runTestCommand(t, svn, project, "status", "--no-ignore")
	for _, expected := range []string{"Config", "Content", "Source", "FlappyBird.uproject"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("status does not contain shareable item %q:\n%s", expected, status)
		}
	}
	for _, excluded := range []string{"I       Binaries", "I       Intermediate", "I       Saved", "I       FlappyBird.sln", "I       .vsconfig"} {
		if !strings.Contains(status, excluded) {
			t.Fatalf("status does not show excluded item %q as ignored:\n%s", excluded, status)
		}
	}
}

func absoluteTargets(workingCopy string, targets []string) []string {
	result := make([]string, 0, len(targets))
	for _, target := range targets {
		result = append(result, filepath.Join(workingCopy, target))
	}
	return result
}

func runTestCommand(t *testing.T, executable, dir string, args ...string) string {
	t.Helper()
	command := exec.Command(executable, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", executable, args, err, output)
	}
	return string(output)
}

func mustWriteTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testRepositoryFileURL(path string) string {
	slashPath := filepath.ToSlash(path)
	if filepath.Separator == '\\' {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}
