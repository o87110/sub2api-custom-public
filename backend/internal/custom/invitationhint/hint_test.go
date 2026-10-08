package invitationhint

import "testing"

func TestNormalize(t *testing.T) {
	text, link, err := Normalize("  获取邀请码  ", "  https://example.com/invite?source=register#steps  ")
	if err != nil || text != "获取邀请码" || link != "https://example.com/invite?source=register#steps" {
		t.Fatalf("Normalize() = %q, %q, %v", text, link, err)
	}

	for _, invalid := range []string{
		"/invite", "javascript:alert(1)", "ftp://example.com", "https://", "https://user@example.com/invite", "https://example.com\\@evil.test",
	} {
		if _, _, err := Normalize("获取邀请码", invalid); err == nil {
			t.Errorf("Normalize() accepted invalid URL %q", invalid)
		}
		if got := SafeURL(invalid); got != "" {
			t.Errorf("SafeURL(%q) = %q", invalid, got)
		}
	}

	if text, link, err := Normalize(" ", " "); err != nil || text != "" || link != "" {
		t.Fatalf("Normalize(empty) = %q, %q, %v", text, link, err)
	}
}
