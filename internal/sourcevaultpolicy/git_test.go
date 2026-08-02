package sourcevaultpolicy

import (
	"strings"
	"testing"
)

func TestControlledGitEnvironmentExcludesInheritedGitAuthority(t *testing.T) {
	t.Setenv("GIT_DIR", "untrusted")
	t.Setenv("GIT_WORK_TREE", "untrusted")
	for _, value := range ControlledGitEnvironment() {
		key, _, _ := strings.Cut(value, "=")
		if key == "GIT_DIR" || key == "GIT_WORK_TREE" {
			t.Fatalf("inherited Git authority remained: %q", value)
		}
	}
}
