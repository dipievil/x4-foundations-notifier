package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	for _, key := range []string{"X4_LISTEN", "X4_TOKEN", "NTFY_TOPIC", "NTFY_TOKEN", "KEEP_ME"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	t.Setenv("KEEP_ME", "from-shell")

	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\n" +
		"\n" +
		"  X4_LISTEN = 0.0.0.0:9090 \r\n" +
		"X4_TOKEN=\"s3cret\"\n" +
		"NTFY_TOPIC=x4-fleet\n" +
		"NTFY_TOKEN='quoted'\n" +
		"KEEP_ME=from-file\n" +
		"broken-line-without-equals\n"

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}

	cases := map[string]string{
		"X4_LISTEN":  "0.0.0.0:9090",
		"X4_TOKEN":   "s3cret",
		"NTFY_TOPIC": "x4-fleet",
		"NTFY_TOKEN": "quoted",
		"KEEP_ME":    "from-shell", // already set: must win over the file
	}
	for key, want := range cases {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s: got %q, want %q", key, got, want)
		}
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	err := loadDotEnv(filepath.Join(t.TempDir(), "does-not-exist"))
	if !os.IsNotExist(err) {
		t.Fatalf("got %v, want a not-exist error", err)
	}
}
