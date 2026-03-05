package startup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"litesync/internal/api"
)

const (
	windowsRunKey   = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	windowsValue    = "LiteSync"
	macosAgentName  = "com.litesync.app"
	linuxDesktopKey = "litesync.desktop"
)

type Service struct {
	provider string
}

func New() *Service {
	return &Service{provider: detectProvider()}
}

func (s *Service) Enable(ctx context.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return api.Wrap(api.ErrInternal, fmt.Sprintf("resolve executable failed: %v", err))
	}
	exe = filepath.Clean(exe)

	switch runtime.GOOS {
	case "windows":
		return s.enableWindows(ctx, exe)
	case "darwin":
		return s.enableDarwin(exe)
	case "linux":
		return s.enableLinux(exe)
	default:
		return api.Wrap(api.ErrNotSupported, "startup enable not supported on this platform")
	}
}

func (s *Service) Disable(ctx context.Context) error {
	switch runtime.GOOS {
	case "windows":
		return s.disableWindows(ctx)
	case "darwin":
		return s.disableDarwin()
	case "linux":
		return s.disableLinux()
	default:
		return api.Wrap(api.ErrNotSupported, "startup disable not supported on this platform")
	}
}

func (s *Service) Status(ctx context.Context) (api.StartupStatus, error) {
	switch runtime.GOOS {
	case "windows":
		enabled, err := s.statusWindows(ctx)
		if err != nil {
			return api.StartupStatus{}, err
		}
		return api.StartupStatus{Enabled: enabled, Provider: s.provider}, nil
	case "darwin":
		enabled, err := s.statusDarwin()
		if err != nil {
			return api.StartupStatus{}, err
		}
		return api.StartupStatus{Enabled: enabled, Provider: s.provider}, nil
	case "linux":
		enabled, err := s.statusLinux()
		if err != nil {
			return api.StartupStatus{}, err
		}
		return api.StartupStatus{Enabled: enabled, Provider: s.provider}, nil
	default:
		return api.StartupStatus{Enabled: false, Provider: s.provider}, api.Wrap(api.ErrNotSupported, "startup status not supported on this platform")
	}
}

func (s *Service) enableWindows(ctx context.Context, exe string) error {
	arg := fmt.Sprintf(`"%s"`, exe)
	cmd := exec.CommandContext(ctx, "reg", "add", windowsRunKey, "/v", windowsValue, "/t", "REG_SZ", "/d", arg, "/f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if isPermissionText(string(out)) {
			return api.Wrap(api.ErrPermissionDenied, "enable windows startup denied")
		}
		return api.Wrap(api.ErrIOTransient, fmt.Sprintf("enable windows startup failed: %s", strings.TrimSpace(string(out))))
	}
	return nil
}

func (s *Service) disableWindows(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "reg", "delete", windowsRunKey, "/v", windowsValue, "/f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// 不存在视为幂等成功
		text := strings.ToLower(string(out))
		if strings.Contains(text, "unable to find") || strings.Contains(text, "cannot find") {
			return nil
		}
		if isPermissionText(string(out)) {
			return api.Wrap(api.ErrPermissionDenied, "disable windows startup denied")
		}
		return api.Wrap(api.ErrIOTransient, fmt.Sprintf("disable windows startup failed: %s", strings.TrimSpace(string(out))))
	}
	return nil
}

func (s *Service) statusWindows(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, "reg", "query", windowsRunKey, "/v", windowsValue)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.ToLower(string(out))
		if strings.Contains(text, "unable to find") || strings.Contains(text, "cannot find") {
			return false, nil
		}
		if isPermissionText(string(out)) {
			return false, api.Wrap(api.ErrPermissionDenied, "query windows startup denied")
		}
		return false, api.Wrap(api.ErrIOTransient, fmt.Sprintf("query windows startup failed: %s", strings.TrimSpace(string(out))))
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(windowsValue)), nil
}

func (s *Service) enableDarwin(exe string) error {
	path, err := macosAgentPath()
	if err != nil {
		return api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("resolve launch agent path failed: %v", err))
	}

	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, macosAgentName, xmlEscape(exe))

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("mkdir launchagents failed: %v", err))
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		if os.IsPermission(err) {
			return api.Wrap(api.ErrPermissionDenied, "write launch agent denied")
		}
		return api.Wrap(api.ErrIOTransient, fmt.Sprintf("write launch agent failed: %v", err))
	}
	return nil
}

func (s *Service) disableDarwin() error {
	path, err := macosAgentPath()
	if err != nil {
		return api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("resolve launch agent path failed: %v", err))
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		if os.IsPermission(err) {
			return api.Wrap(api.ErrPermissionDenied, "remove launch agent denied")
		}
		return api.Wrap(api.ErrIOTransient, fmt.Sprintf("remove launch agent failed: %v", err))
	}
	return nil
}

func (s *Service) statusDarwin() (bool, error) {
	path, err := macosAgentPath()
	if err != nil {
		return false, api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("resolve launch agent path failed: %v", err))
	}
	_, err = os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		if os.IsPermission(err) {
			return false, api.Wrap(api.ErrPermissionDenied, "stat launch agent denied")
		}
		return false, api.Wrap(api.ErrIOTransient, fmt.Sprintf("stat launch agent failed: %v", err))
	}
	return true, nil
}

func (s *Service) enableLinux(exe string) error {
	path, err := linuxAutostartPath()
	if err != nil {
		return api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("resolve autostart path failed: %v", err))
	}
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Version=1.0
Name=LiteSync
Comment=LiteSync backup service
Exec=%s
X-GNOME-Autostart-enabled=true
`, shellEscapeDesktopExec(exe))

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("mkdir autostart failed: %v", err))
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		if os.IsPermission(err) {
			return api.Wrap(api.ErrPermissionDenied, "write autostart file denied")
		}
		return api.Wrap(api.ErrIOTransient, fmt.Sprintf("write autostart file failed: %v", err))
	}
	return nil
}

func (s *Service) disableLinux() error {
	path, err := linuxAutostartPath()
	if err != nil {
		return api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("resolve autostart path failed: %v", err))
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		if os.IsPermission(err) {
			return api.Wrap(api.ErrPermissionDenied, "remove autostart file denied")
		}
		return api.Wrap(api.ErrIOTransient, fmt.Sprintf("remove autostart file failed: %v", err))
	}
	return nil
}

func (s *Service) statusLinux() (bool, error) {
	path, err := linuxAutostartPath()
	if err != nil {
		return false, api.Wrap(api.ErrPermissionDenied, fmt.Sprintf("resolve autostart path failed: %v", err))
	}
	_, err = os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		if os.IsPermission(err) {
			return false, api.Wrap(api.ErrPermissionDenied, "stat autostart file denied")
		}
		return false, api.Wrap(api.ErrIOTransient, fmt.Sprintf("stat autostart file failed: %v", err))
	}
	return true, nil
}

func detectProvider() string {
	switch runtime.GOOS {
	case "windows":
		return "windows_run_registry"
	case "darwin":
		return "macos_launch_agent"
	case "linux":
		return "linux_autostart_desktop"
	default:
		return "unknown"
	}
}

func isPermissionText(text string) bool {
	l := strings.ToLower(text)
	return strings.Contains(l, "access is denied") || strings.Contains(l, "permission denied")
}

func macosAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", macosAgentName+".plist"), nil
}

func linuxAutostartPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart", linuxDesktopKey), nil
}

func xmlEscape(v string) string {
	v = strings.ReplaceAll(v, "&", "&amp;")
	v = strings.ReplaceAll(v, "<", "&lt;")
	v = strings.ReplaceAll(v, ">", "&gt;")
	v = strings.ReplaceAll(v, `"`, "&quot;")
	return v
}

func shellEscapeDesktopExec(v string) string {
	if strings.ContainsAny(v, " \t") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}
