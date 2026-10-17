package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The same cases as Farol's DiffTests on the Mac.

const sampleDiff = `diff --git a/Sources/app.swift b/Sources/app.swift
index 1111111..2222222 100644
--- a/Sources/app.swift
+++ b/Sources/app.swift
@@ -10,4 +10,5 @@ struct App {
 let a = 1
-let b = 2
+let b = 3
+let c = 4
 let d = 5
diff --git a/old name.txt b/new name.txt
similarity index 90%
rename from old name.txt
rename to new name.txt
diff --git a/gone.txt b/gone.txt
deleted file mode 100644
--- a/gone.txt
+++ /dev/null
@@ -1 +0,0 @@
-bye
\ No newline at end of file
diff --git a/logo.png b/logo.png
Binary files a/logo.png and b/logo.png differ
`

func paths(files []change) []string {
	var list []string
	for _, f := range files {
		list = append(list, f.Path)
	}
	return list
}

func TestParsesFilesAndHunks(t *testing.T) {
	files := parseDiff(sampleDiff)
	if got := paths(files); !slices.Equal(got, []string{"Sources/app.swift", "new name.txt", "gone.txt", "logo.png"}) {
		t.Fatalf("paths %v", got)
	}
	if app := files[0]; app.Status != "modified" || app.Added != 2 || app.Removed != 1 {
		t.Fatalf("app %+v", app)
	}
	if got := unified(files[0]); got != "@@ -10 +10 @@\n let a = 1\n-let b = 2\n+let b = 3\n+let c = 4\n let d = 5\n" {
		t.Fatalf("unified %q", got)
	}
	if files[1].Status != "renamed" || files[1].OldPath != "old name.txt" || files[2].Status != "deleted" || files[2].Removed != 1 || !files[3].Binary {
		t.Fatalf("files %+v", files[1:])
	}
}

// sandbox is a repository on main with one commit.
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "--quiet", "-b", "main"}, {"commit", "--quiet", "--allow-empty", "-m", "start"}} {
		gitIn(t, dir, args...)
	}
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Farol", "-c", "user.email=farol@example.com"}, args...)...)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s", args, out)
	}
}

func commitAll(t *testing.T, dir, message string) {
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "--quiet", "-m", message)
}

func TestUncommittedIncludesUntrackedFiles(t *testing.T) {
	dir := sandbox(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o600)
	commitAll(t, dir, "a")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n2\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\nfile\nhere\n"), 0o600)
	files, err := changes(dir, "")
	if err != nil || !slices.Equal(paths(files), []string{"a.txt", "b.txt"}) || files[1].Status != "added" || files[1].Added != 3 {
		t.Fatalf("changes %+v %v", files, err)
	}
}

func TestOnABranchItIsEverythingSinceTheBase(t *testing.T) {
	dir := sandbox(t)
	if base, label := scope(dir); base != "" || label != "Uncommitted" {
		t.Fatalf("on main: %q %q", base, label)
	}
	gitIn(t, dir, "checkout", "--quiet", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("committed\n"), 0o600)
	commitAll(t, dir, "c")
	os.WriteFile(filepath.Join(dir, "d.txt"), []byte("not yet\n"), 0o600)
	base, label := scope(dir)
	if base != "main" || label != "Since main" {
		t.Fatalf("on feat: %q %q", base, label)
	}
	if files, _ := changes(dir, base); !slices.Equal(paths(files), []string{"c.txt", "d.txt"}) {
		t.Fatalf("since main %v", paths(files))
	}
	if files, _ := changes(dir, ""); !slices.Equal(paths(files), []string{"d.txt"}) {
		t.Fatalf("uncommitted %v", paths(files))
	}
}

func TestAPhoneReadsTheChangesAndOneFile(t *testing.T) {
	b := newBench(t)
	b.ready()
	b.terminal.agent.folder = sandbox(t)
	os.WriteFile(filepath.Join(b.terminal.agent.folder, "notes.md"), []byte("hello\n"), 0o600)
	same(t, b.ask("c", fmt.Sprintf(`{"changes":{"session":%q}}`, fakeSession)),
		changesEvent(fakeSession, "Uncommitted", []change{{Path: "notes.md", Status: "added", Added: 1}}))
	same(t, b.ask("d", fmt.Sprintf(`{"diff":{"session":%q,"path":"notes.md"}}`, fakeSession)), diffEvent(fakeSession, "notes.md", "@@ -0 +1 @@\n+hello\n"))
	same(t, b.ask("gone", fmt.Sprintf(`{"diff":{"session":%q,"path":"other.md"}}`, fakeSession)), refusedEvent("gone", "That file has no changes any more."))
	b.terminal.agent.folder = t.TempDir()
	if told := b.ask("none", fmt.Sprintf(`{"changes":{"session":%q}}`, fakeSession)); !strings.Contains(told[0], "not in a git repository") {
		t.Fatalf("outside git: %v", told)
	}
}
