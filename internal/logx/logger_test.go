package logx

import (
	"bytes"
	"strings"
	"testing"

	"litesync/internal/api"
)

func TestErrorIncludesErrorCode(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter("debug", &buf)

	l.Error("test", api.Wrap(api.ErrInvalidArgument, "bad input"))

	out := buf.String()
	if !strings.Contains(out, "error_code=INVALID_ARGUMENT") {
		t.Fatalf("expected error_code in log output, got: %s", out)
	}
}
