package types

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeRemoteURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "https", in: "https://github.com/lewtec/rod-mcp.git", want: "github.com/lewtec/rod-mcp"},
		{name: "https no git suffix", in: "https://github.com/lewtec/rod-mcp", want: "github.com/lewtec/rod-mcp"},
		{name: "https trailing slash", in: "https://github.com/lewtec/rod-mcp.git/", want: "github.com/lewtec/rod-mcp"},
		{name: "https user", in: "https://git@github.com/lewtec/rod-mcp.git", want: "github.com/lewtec/rod-mcp"},
		{name: "ssh scheme", in: "ssh://git@github.com/lewtec/rod-mcp.git", want: "github.com/lewtec/rod-mcp"},
		{name: "ssh scheme port", in: "ssh://git@github.com:22/lewtec/rod-mcp.git", want: "github.com/lewtec/rod-mcp"},
		{name: "scp", in: "git@github.com:lewtec/rod-mcp.git", want: "github.com/lewtec/rod-mcp"},
		{name: "scp no suffix", in: "git@github.com:lewtec/rod-mcp", want: "github.com/lewtec/rod-mcp"},
		{name: "empty", in: "  ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeRemoteURL(tt.in); got != tt.want {
				t.Fatalf("NormalizeRemoteURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeProfileSlug(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain", in: "apple-music", want: "apple-music"},
		{name: "spaces and slash", in: "Foo Bar/baz", want: "Foo-Bar-baz"},
		{name: "trim", in: "  keep.me_1  ", want: "keep.me_1"},
		{name: "empty", in: "   ", wantErr: true},
		{name: "dots", in: "...", wantErr: true},
		{name: "dot", in: ".", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := SanitizeProfileSlug(tt.in)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidProfileSlug) {
					t.Fatalf("SanitizeProfileSlug(%q) = %q, %v, want %v", tt.in, got, err, ErrInvalidProfileSlug)
				}
				return
			}
			if err != nil {
				t.Fatalf("SanitizeProfileSlug(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("SanitizeProfileSlug(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDefaultProfileIdentityGitRemote(t *testing.T) {
	orig := GitOrigin
	GitOrigin = func(string) string { return "git@github.com:lewtec/rod-mcp.git" }
	t.Cleanup(func() { GitOrigin = orig })

	key, name := DefaultProfileIdentity()
	wantKey := "git:github.com/lewtec/rod-mcp"
	if key != wantKey {
		t.Fatalf("key = %q, want %q", key, wantKey)
	}
	if name != HashedProfileName(wantKey) {
		t.Fatalf("name = %q, want %q", name, HashedProfileName(wantKey))
	}
}

func TestDefaultProfileIdentityCwdFallback(t *testing.T) {
	orig := GitOrigin
	GitOrigin = func(string) string { return "" }
	t.Cleanup(func() { GitOrigin = orig })

	dir := t.TempDir()
	t.Chdir(dir)
	abs, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}

	key, name := DefaultProfileIdentity()
	wantKey := "cwd:" + filepath.Clean(abs)
	if key != wantKey {
		t.Fatalf("key = %q, want %q", key, wantKey)
	}
	if name != HashedProfileName(wantKey) {
		t.Fatalf("name = %q, want %q", name, HashedProfileName(wantKey))
	}
}

func TestWriteProfileSourceSidecar(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profiles", "abc123")
	writeProfileSource(profile, "git:github.com/lewtec/rod-mcp")

	got, err := os.ReadFile(profile + ".source")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "git:github.com/lewtec/rod-mcp\n" {
		t.Fatalf("sidecar = %q", got)
	}
}

func TestWriteProfileSourceEmptyKeyIsNoop(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profiles", "abc123")
	writeProfileSource(profile, "")
	if _, err := os.Stat(profile + ".source"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat sidecar: %v, want not exist", err)
	}
}
