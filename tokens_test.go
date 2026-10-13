package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGeneratedTokensPersistAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	admin, download, err := resolveTokens(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(admin) < 26 || len(download) < 26 || admin == download {
		t.Fatal("generated credentials must be non-empty, sufficiently long and distinct")
	}
	restartedAdmin, restartedDownload, err := resolveTokens(path, "", "")
	if err != nil || restartedAdmin != admin || restartedDownload != download {
		t.Fatal("saved tokens changed on restart")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("generated credentials file must have mode 0600")
		}
	}
}

func TestGenerateOnlyMissingToken(t *testing.T) {
	for _, missing := range []string{"admin", "download"} {
		t.Run(missing, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tokens.json")
			admin, download := "explicit-admin", "explicit-download"
			if missing == "admin" {
				admin = ""
			} else {
				download = ""
			}
			resolvedAdmin, resolvedDownload, err := resolveTokens(path, admin, download)
			if err != nil {
				t.Fatal(err)
			}
			if (admin != "" && resolvedAdmin != admin) || (download != "" && resolvedDownload != download) {
				t.Fatal("explicit token was replaced")
			}
			if resolvedAdmin == "" || resolvedDownload == "" || resolvedAdmin == resolvedDownload {
				t.Fatal("missing credential was not generated")
			}
		})
	}
}

func TestExplicitTokensOverrideSavedTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	_, savedDownload, err := resolveTokens(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	admin, download, err := resolveTokens(path, "explicit-admin", "")
	if err != nil || admin != "explicit-admin" || download != savedDownload {
		t.Fatal("explicit admin should override only the saved admin")
	}
	if err := os.WriteFile(path, []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	admin, download, err = resolveTokens(path, "explicit-admin", "explicit-download")
	if err != nil || admin != "explicit-admin" || download != "explicit-download" {
		t.Fatal("explicit credentials should not depend on saved token file")
	}
}

func TestInvalidSavedOrIdenticalTokensAreRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	if _, _, err := resolveTokens(path, "same", "same"); err == nil {
		t.Fatal("identical explicit tokens should be rejected")
	}
	for _, data := range []string{"invalid JSON", "null", `{"admin_token":"same","download_token":"same"}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := resolveTokens(path, "", ""); err == nil {
			t.Fatal("invalid saved credentials should not silently regenerate")
		}
		stored, err := os.ReadFile(path)
		if err != nil || string(stored) != data {
			t.Fatal("invalid saved file was overwritten")
		}
	}
}
