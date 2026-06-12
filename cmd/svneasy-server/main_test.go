package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteAuthzRepository(t *testing.T) {
	input := `[groups]
developers = alice,bob

[main:/]
@developers = rw

[main:/private]
alice = rw

[other:/]
guest = r
`
	actual := rewriteAuthzRepository(input, "main", "SnowRacing")
	for _, expected := range []string{"[SnowRacing:/]", "[SnowRacing:/private]", "[other:/]"} {
		if !strings.Contains(actual, expected) {
			t.Fatalf("rewritten authz does not contain %q:\n%s", expected, actual)
		}
	}
	if strings.Contains(actual, "[main:/") {
		t.Fatalf("old repository sections remained:\n%s", actual)
	}
}

func TestUpsertIniKey(t *testing.T) {
	input := `[general]
anon-access = none
auth-access = write

[sasl]
use-sasl = false
`
	actual := upsertIniKey(input, "general", "realm", "SnowRacing")
	if !strings.Contains(actual, "realm = SnowRacing") {
		t.Fatalf("key was not inserted:\n%s", actual)
	}
	if strings.Index(actual, "realm = SnowRacing") > strings.Index(actual, "[sasl]") {
		t.Fatalf("key was inserted into the wrong section:\n%s", actual)
	}

	updated := upsertIniKey(actual, "general", "realm", "NewRealm")
	if strings.Count(updated, "realm = ") != 1 || !strings.Contains(updated, "realm = NewRealm") {
		t.Fatalf("key was not updated in place:\n%s", updated)
	}
}

func TestChooseAuthzSection(t *testing.T) {
	if actual := chooseAuthzSection("[Repo:/]\na = rw\n", "Repo", "/private"); actual != "Repo:/private" {
		t.Fatalf("prefixed section = %q", actual)
	}
	if actual := chooseAuthzSection("[/]\na = rw\n", "Repo", "/private"); actual != "/private" {
		t.Fatalf("local section = %q", actual)
	}
}

func TestIntegrationCreateAndMigrateRepository(t *testing.T) {
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
	source := filepath.Join(root, "main")
	runServerTestCommand(t, svnadmin, root, "create", source)
	mustWriteServerTestFile(t, filepath.Join(source, "conf", "svnserve.conf"), `[general]
anon-access = none
auth-access = write
password-db = passwd
authz-db = authz
realm = main
`)
	mustWriteServerTestFile(t, filepath.Join(source, "conf", "passwd"), `[users]
alice = secret
`)
	mustWriteServerTestFile(t, filepath.Join(source, "conf", "authz"), `[groups]
developers = alice

[main:/]
@developers = rw
`)

	server := server{root: root, svn: svn, svnadmin: svnadmin}
	if err := server.create(createOptions{
		source:       "main",
		name:         "SnowRacing",
		createLayout: true,
	}); err != nil {
		t.Fatal(err)
	}

	newRepository := filepath.Join(root, "SnowRacing")
	authz, err := os.ReadFile(filepath.Join(newRepository, "conf", "authz"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(authz), "[SnowRacing:/]") {
		t.Fatalf("authz was not rewritten:\n%s", authz)
	}
	config, err := os.ReadFile(filepath.Join(newRepository, "conf", "svnserve.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "realm = SnowRacing") {
		t.Fatalf("realm was not rewritten:\n%s", config)
	}

	repositoryURL := repositoryFileURL(newRepository)
	listing := runServerTestCommand(t, svn, root, "list", repositoryURL)
	for _, directory := range []string{"branches/", "tags/", "trunk/"} {
		if !strings.Contains(listing, directory) {
			t.Fatalf("repository listing does not contain %q:\n%s", directory, listing)
		}
	}

	if err := server.addOrUpdateUser("SnowRacing", "bob", "new-secret", "r"); err != nil {
		t.Fatal(err)
	}
	passwords, err := os.ReadFile(filepath.Join(newRepository, "conf", "passwd"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(passwords), "bob = new-secret") {
		t.Fatalf("user was not added:\n%s", passwords)
	}
	updatedAuthz, err := os.ReadFile(filepath.Join(newRepository, "conf", "authz"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updatedAuthz), "bob = r") {
		t.Fatalf("permission was not added:\n%s", updatedAuthz)
	}
}

func runServerTestCommand(t *testing.T, executable, dir string, args ...string) string {
	t.Helper()
	command := exec.Command(executable, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", executable, args, err, output)
	}
	return string(output)
}

func mustWriteServerTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
