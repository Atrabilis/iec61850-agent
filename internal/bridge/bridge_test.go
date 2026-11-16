package bridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := Decode([]byte("not json")); err == nil {
		t.Error("expected an error for invalid JSON")
	}
	if _, err := Decode([]byte(`{"meta":{}}`)); err == nil {
		t.Error("expected an error when domains is missing")
	}
}

// fakeReader writes a shell script that behaves like iec61850-read with the given output and exit code.
func fakeReader(t *testing.T, stdout string, code int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "iec61850-read")
	script := "#!/bin/sh\ncat <<'EOF'\n" + stdout + "\nEOF\nexit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadExitCodes(t *testing.T) {
	ok := `{"domains":{"LD0":{}},"meta":{"reads":1,"failures":0}}`
	partial := `{"domains":{"LD0":{}},"meta":{"reads":2,"failures":1}}`
	opts := func(path string) Options {
		return Options{ReaderPath: path, Host: "h", Port: 102, RunTimeout: 10 * time.Second}
	}

	if s, err := Read(context.Background(), opts(fakeReader(t, ok, 0))); err != nil || s.Meta.Reads != 1 {
		t.Fatalf("exit 0: %v %+v", err, s)
	}
	// Exit 3 = some reads failed: the snapshot is still returned.
	if s, err := Read(context.Background(), opts(fakeReader(t, partial, 3))); err != nil || s.Meta.Failures != 1 {
		t.Fatalf("exit 3: %v %+v", err, s)
	}
	// Exit 1 = connection error.
	if _, err := Read(context.Background(), opts(fakeReader(t, "", 1))); err == nil {
		t.Fatal("exit 1: expected an error")
	}
}
