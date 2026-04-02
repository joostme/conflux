package env

import (
	"os"
	"testing"

	"github.com/joho/godotenv"
)

func TestMergeAndExpand_BasicExpansion(t *testing.T) {
	r := &Resolver{}
	resolved, err := r.mergeAndExpand([][]byte{
		[]byte("MY_SECRET=s3cret\n"),
		[]byte("DB_URL=postgres://user:${MY_SECRET}@db:5432/mydb\n"),
	})
	if err != nil {
		t.Fatalf("mergeAndExpand() error = %v", err)
	}
	defer func() { _ = r.Cleanup() }()

	got, err := godotenv.Read(resolved)
	if err != nil {
		t.Fatalf("reading resolved file: %v", err)
	}

	if got["MY_SECRET"] != "s3cret" {
		t.Errorf("MY_SECRET = %q, want %q", got["MY_SECRET"], "s3cret")
	}
	if got["DB_URL"] != "postgres://user:s3cret@db:5432/mydb" {
		t.Errorf("DB_URL = %q, want %q", got["DB_URL"], "postgres://user:s3cret@db:5432/mydb")
	}
}

func TestMergeAndExpand_LastWinsPrecedence(t *testing.T) {
	r := &Resolver{}
	resolved, err := r.mergeAndExpand([][]byte{
		[]byte("PORT=8080\n"),
		[]byte("PORT=9090\n"),
	})
	if err != nil {
		t.Fatalf("mergeAndExpand() error = %v", err)
	}
	defer func() { _ = r.Cleanup() }()

	got, err := godotenv.Read(resolved)
	if err != nil {
		t.Fatalf("reading resolved file: %v", err)
	}

	if got["PORT"] != "9090" {
		t.Errorf("PORT = %q, want %q (last wins)", got["PORT"], "9090")
	}
}

func TestMergeAndExpand_ConcatenationWithSecret(t *testing.T) {
	r := &Resolver{}
	resolved, err := r.mergeAndExpand([][]byte{
		[]byte("PASSWORD=hunter2\nHOST=db.example.com\n"),
		[]byte("CONNECTION=mysql://admin:${PASSWORD}@${HOST}:3306/app\n"),
	})
	if err != nil {
		t.Fatalf("mergeAndExpand() error = %v", err)
	}
	defer func() { _ = r.Cleanup() }()

	got, err := godotenv.Read(resolved)
	if err != nil {
		t.Fatalf("reading resolved file: %v", err)
	}

	want := "mysql://admin:hunter2@db.example.com:3306/app"
	if got["CONNECTION"] != want {
		t.Errorf("CONNECTION = %q, want %q", got["CONNECTION"], want)
	}
}

func TestMergeAndExpand_NoExpansionNeeded(t *testing.T) {
	r := &Resolver{}
	resolved, err := r.mergeAndExpand([][]byte{
		[]byte("FOO=bar\nBAZ=qux\n"),
	})
	if err != nil {
		t.Fatalf("mergeAndExpand() error = %v", err)
	}
	defer func() { _ = r.Cleanup() }()

	got, err := godotenv.Read(resolved)
	if err != nil {
		t.Fatalf("reading resolved file: %v", err)
	}

	if got["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", got["FOO"], "bar")
	}
	if got["BAZ"] != "qux" {
		t.Errorf("BAZ = %q, want %q", got["BAZ"], "qux")
	}
}

func TestMergeAndExpand_EmptyContents(t *testing.T) {
	r := &Resolver{}
	resolved, err := r.mergeAndExpand(nil)
	if err != nil {
		t.Fatalf("mergeAndExpand() error = %v", err)
	}
	if resolved != "" {
		t.Errorf("expected empty string for no contents, got %q", resolved)
	}
}

func TestMergeAndExpand_CleanupRemovesTempFile(t *testing.T) {
	r := &Resolver{}
	resolved, err := r.mergeAndExpand([][]byte{
		[]byte("KEY=value\n"),
	})
	if err != nil {
		t.Fatalf("mergeAndExpand() error = %v", err)
	}

	// File should exist before cleanup.
	if _, err := os.Stat(resolved); err != nil {
		t.Fatalf("resolved file should exist: %v", err)
	}

	if err := r.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	// File should be gone after cleanup.
	if _, err := os.Stat(resolved); !os.IsNotExist(err) {
		t.Errorf("resolved temp file should be removed after Cleanup, err = %v", err)
	}
}

func TestMergeAndExpand_UndefinedVarExpandsToEmpty(t *testing.T) {
	r := &Resolver{}
	resolved, err := r.mergeAndExpand([][]byte{
		[]byte("URL=prefix-${UNDEFINED_VAR}-suffix\n"),
	})
	if err != nil {
		t.Fatalf("mergeAndExpand() error = %v", err)
	}
	defer func() { _ = r.Cleanup() }()

	got, err := godotenv.Read(resolved)
	if err != nil {
		t.Fatalf("reading resolved file: %v", err)
	}

	if got["URL"] != "prefix--suffix" {
		t.Errorf("URL = %q, want %q", got["URL"], "prefix--suffix")
	}
}

func TestMergeAndExpand_MissingTrailingNewline(t *testing.T) {
	r := &Resolver{}
	resolved, err := r.mergeAndExpand([][]byte{
		[]byte("A=1"),      // no trailing newline
		[]byte("B=${A}_2"), // no trailing newline, references A
	})
	if err != nil {
		t.Fatalf("mergeAndExpand() error = %v", err)
	}
	defer func() { _ = r.Cleanup() }()

	got, err := godotenv.Read(resolved)
	if err != nil {
		t.Fatalf("reading resolved file: %v", err)
	}

	if got["A"] != "1" {
		t.Errorf("A = %q, want %q", got["A"], "1")
	}
	if got["B"] != "1_2" {
		t.Errorf("B = %q, want %q", got["B"], "1_2")
	}
}
