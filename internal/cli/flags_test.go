package cli

import (
	"os"
	"testing"
)

func TestEnvIntValues(t *testing.T) {
	_ = os.Setenv("DSH_CLI_ENVINT_X", "42")
	defer func() { _ = os.Unsetenv("DSH_CLI_ENVINT_X") }()
	if got := envInt("DSH_CLI_ENVINT_X", 0); got != 42 {
		t.Errorf("envInt = %d, want 42", got)
	}
	if got := envInt("DSH_CLI_NONEXISTENT", 7); got != 7 {
		t.Errorf("envInt 缺失应返回默认: %d", got)
	}
	_ = os.Setenv("DSH_CLI_ENVINT_BAD", "notanum")
	defer func() { _ = os.Unsetenv("DSH_CLI_ENVINT_BAD") }()
	if got := envInt("DSH_CLI_ENVINT_BAD", 5); got != 5 {
		t.Errorf("envInt 非法值应返回默认: %d", got)
	}
}

func TestEnvBoolValues(t *testing.T) {
	_ = os.Setenv("DSH_CLI_ENVBOOL_X", "true")
	defer func() { _ = os.Unsetenv("DSH_CLI_ENVBOOL_X") }()
	if !envBool("DSH_CLI_ENVBOOL_X") {
		t.Error("envBool true 应返回 true")
	}
	if envBool("DSH_CLI_NONEXISTENT") {
		t.Error("envBool 缺失应返回 false")
	}
	_ = os.Setenv("DSH_CLI_ENVBOOL_Y", "false")
	defer func() { _ = os.Unsetenv("DSH_CLI_ENVBOOL_Y") }()
	if envBool("DSH_CLI_ENVBOOL_Y") {
		t.Error("envBool false 应返回 false")
	}
}
