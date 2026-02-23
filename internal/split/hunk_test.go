package split

import (
	"strings"
	"testing"
)

const testDiffTwoHunks = `diff --git a/user.go b/user.go
index abc123..def456 100644
--- a/user.go
+++ b/user.go
@@ -1,5 +1,6 @@
 package models

 type User struct {
+	Email string
 	Name  string
 }
@@ -10,3 +11,7 @@ func (u *User) String() string {
 	return u.Name
 }
+
+func (u *User) Validate() error {
+	return nil
+}
`

const testDiffTwoFiles = `diff --git a/one.go b/one.go
index aaa..bbb 100644
--- a/one.go
+++ b/one.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
 func main() {}
diff --git a/two.go b/two.go
index ccc..ddd 100644
--- a/two.go
+++ b/two.go
@@ -1,2 +1,3 @@
 package main
+func helper() {}
`

func TestParseDiff_SingleFileTwoHunks(t *testing.T) {
	files := ParseDiff(testDiffTwoHunks)
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	fd := files[0]
	if fd.Path != "user.go" {
		t.Errorf("path: got %q, want %q", fd.Path, "user.go")
	}
	if len(fd.Hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(fd.Hunks))
	}
	if !strings.HasPrefix(fd.Hunks[0].Header, "@@ -1,5 +1,6") {
		t.Errorf("hunk 0 header: %q", fd.Hunks[0].Header)
	}
	if !strings.HasPrefix(fd.Hunks[1].Header, "@@ -10,3 +11,7") {
		t.Errorf("hunk 1 header: %q", fd.Hunks[1].Header)
	}
	if !strings.Contains(fd.Hunks[0].Body, "Email") {
		t.Error("hunk 0 body should contain Email")
	}
	if !strings.Contains(fd.Hunks[1].Body, "Validate") {
		t.Error("hunk 1 body should contain Validate")
	}
}

func TestParseDiff_TwoFiles(t *testing.T) {
	files := ParseDiff(testDiffTwoFiles)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Path != "one.go" {
		t.Errorf("file 0 path: %q", files[0].Path)
	}
	if files[1].Path != "two.go" {
		t.Errorf("file 1 path: %q", files[1].Path)
	}
	if len(files[0].Hunks) != 1 {
		t.Errorf("file 0 hunks: got %d, want 1", len(files[0].Hunks))
	}
	if len(files[1].Hunks) != 1 {
		t.Errorf("file 1 hunks: got %d, want 1", len(files[1].Hunks))
	}
}

func TestParseDiff_Empty(t *testing.T) {
	files := ParseDiff("")
	if len(files) != 0 {
		t.Errorf("expected 0 files for empty diff, got %d", len(files))
	}
}

func TestFilterHunks_SelectOne(t *testing.T) {
	files := ParseDiff(testDiffTwoHunks)
	fd := files[0]

	patch := FilterHunks(fd, []int{1})

	if !strings.Contains(patch, "diff --git") {
		t.Error("patch should have file header")
	}
	if !strings.Contains(patch, "Validate") {
		t.Error("patch should contain hunk 1 content")
	}
	if strings.Contains(patch, "Email") {
		t.Error("patch should NOT contain hunk 0 content")
	}
}

func TestFilterHunks_SelectAll(t *testing.T) {
	files := ParseDiff(testDiffTwoHunks)
	fd := files[0]

	all := FilterHunks(fd, []int{0, 1})
	if !strings.Contains(all, "Email") {
		t.Error("all-hunks patch should contain hunk 0")
	}
	if !strings.Contains(all, "Validate") {
		t.Error("all-hunks patch should contain hunk 1")
	}
}

func TestFormatNumberedHunks(t *testing.T) {
	files := ParseDiff(testDiffTwoHunks)
	fd := files[0]

	formatted := FormatNumberedHunks(fd)
	if !strings.Contains(formatted, "Hunk 0:") {
		t.Error("should contain Hunk 0 label")
	}
	if !strings.Contains(formatted, "Hunk 1:") {
		t.Error("should contain Hunk 1 label")
	}
	if !strings.Contains(formatted, "Email") {
		t.Error("should contain hunk 0 content")
	}
	if !strings.Contains(formatted, "Validate") {
		t.Error("should contain hunk 1 content")
	}
}
