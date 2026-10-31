package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFile writes a plain .nzb or a zip containing the .nzb
func TestWriteFile(t *testing.T) {
	dir := t.TempDir()

	// plain file
	path, err := writeFile(dir, "plain", "<nzb/>", false, "unused")
	if err != nil {
		t.Fatalf("writeFile plain: %v", err)
	}
	if filepath.Ext(path) != ".nzb" {
		t.Errorf("path = %q, want .nzb extension", path)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "<nzb/>" {
		t.Errorf("plain file content = %q, err=%v", data, err)
	}

	// zipped file
	path, err = writeFile(dir, "inner", "<nzb/>", true, "archive")
	if err != nil {
		t.Fatalf("writeFile zip: %v", err)
	}
	if filepath.Ext(path) != ".zip" {
		t.Errorf("path = %q, want .zip extension", path)
	}
}

// cleanup must delete old .nzb AND .zip files but keep recent ones and
// other file types (regression test: .zip files were never cleaned)
func TestDeleteFiles(t *testing.T) {
	dir := t.TempDir()
	conf.Execute.Category_folder = false
	conf.Execute.CleanUpMaxAge = 1 // 1 day

	old := time.Now().Add(-48 * time.Hour)
	mkFile := func(name string, age time.Time) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, age, age); err != nil {
			t.Fatal(err)
		}
		return p
	}

	oldNzb := mkFile("old.nzb", old)
	oldZip := mkFile("old.zip", old)
	oldTxt := mkFile("old.txt", old)
	newNzb := mkFile("new.nzb", time.Now())

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	delete_files(entries, dir, 0)

	for _, p := range []string{oldNzb, oldZip} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted", filepath.Base(p))
		}
	}
	for _, p := range []string{oldTxt, newNzb} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to be kept", filepath.Base(p))
		}
	}
}

// with category folders enabled, .nzb files one level deep are cleaned too
func TestDeleteFilesCategoryFolder(t *testing.T) {
	dir := t.TempDir()
	conf.Execute.Category_folder = true
	conf.Execute.CleanUpMaxAge = 1

	old := time.Now().Add(-48 * time.Hour)
	sub := filepath.Join(dir, "movies")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(sub, "old.nzb")
	if err := os.WriteFile(nested, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(nested, old, old); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	delete_files(entries, dir, 0)

	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Error("expected nested .nzb in category folder to be deleted")
	}
}
