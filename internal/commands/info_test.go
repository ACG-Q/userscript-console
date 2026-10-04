package commands

import (
	"testing"
)

func TestOrDash(t *testing.T) {
	if got := orDash(""); got != "—" {
		t.Errorf("orDash(\"\") = %q, want —", got)
	}
	if got := orDash("   "); got != "—" {
		t.Errorf("orDash(\"   \") = %q, want —", got)
	}
	if got := orDash("test"); got != "test" {
		t.Errorf("orDash(\"test\") = %q, want test", got)
	}
}

func TestJoinOrDash(t *testing.T) {
	if got := joinOrDash(nil); got != "—" {
		t.Errorf("joinOrDash(nil) = %q, want —", got)
	}
	if got := joinOrDash([]string{}); got != "—" {
		t.Errorf("joinOrDash([]) = %q, want —", got)
	}
	if got := joinOrDash([]string{"a", "b"}); got != "`a` `b`" {
		t.Errorf("joinOrDash([a,b]) = %q, want `a` `b`", got)
	}
}
