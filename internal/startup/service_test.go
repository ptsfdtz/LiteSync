package startup

import (
	"runtime"
	"testing"
)

func TestDetectProvider(t *testing.T) {
	got := detectProvider()
	switch runtime.GOOS {
	case "windows":
		if got != "windows_run_registry" {
			t.Fatalf("unexpected provider: %s", got)
		}
	case "darwin":
		if got != "macos_launch_agent" {
			t.Fatalf("unexpected provider: %s", got)
		}
	case "linux":
		if got != "linux_autostart_desktop" {
			t.Fatalf("unexpected provider: %s", got)
		}
	default:
		if got != "unknown" {
			t.Fatalf("unexpected provider: %s", got)
		}
	}
}

func TestEscapes(t *testing.T) {
	xml := xmlEscape(`C:\Program Files\LiteSync\litesync.exe`)
	if xml == "" {
		t.Fatal("xml escape should not be empty")
	}
	desk := shellEscapeDesktopExec(`/home/user/My App/litesync`)
	if desk[0] != '"' {
		t.Fatalf("expected desktop exec path quoted, got: %s", desk)
	}
}
