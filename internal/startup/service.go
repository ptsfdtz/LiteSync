package startup

import (
	"context"
	"runtime"

	"litesync/internal/api"
)

// Service 是开机自启模块占位实现。
type Service struct {
	provider string
}

func New() *Service {
	return &Service{provider: detectProvider()}
}

func (s *Service) Enable(_ context.Context) error {
	return api.Wrap(api.ErrNotSupported, "startup enable is not implemented")
}

func (s *Service) Disable(_ context.Context) error {
	return api.Wrap(api.ErrNotSupported, "startup disable is not implemented")
}

func (s *Service) Status(_ context.Context) (api.StartupStatus, error) {
	return api.StartupStatus{
		Enabled:  false,
		Provider: s.provider,
	}, nil
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
