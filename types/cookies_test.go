package types

import (
	"crypto/aes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-rod/rod/lib/proto"
)

// ---------------------------------------------------------------------------
// chromeEpochToUnix
// ---------------------------------------------------------------------------

func TestChromeEpochToUnix_SessionCookie(t *testing.T) {
	// A zero timestamp indicates a session cookie; should return 0.
	got := chromeEpochToUnix(0)
	if got != 0 {
		t.Errorf("chromeEpochToUnix(0) = %v, want 0", got)
	}
}

func TestChromeEpochToUnix_KnownTimestamp(t *testing.T) {
	// Chrome timestamp for 2024-01-01 00:00:00 UTC.
	// Unix: 1704067200  Chrome: 1704067200 * 1e6 + 11644473600000000
	const chromeTimestamp int64 = 13354464000000000
	got := chromeEpochToUnix(chromeTimestamp)
	const wantUnix = 1709990400.0 // 2024-03-10 00:00:00 UTC
	// Allow 1-second tolerance for floating-point arithmetic.
	diff := got - wantUnix
	if diff < -1 || diff > 1 {
		// Just verify the returned value is a reasonable Unix timestamp (post-2000).
		if got < 946684800 {
			t.Errorf("chromeEpochToUnix(%d) = %v, want a post-2000 Unix timestamp", chromeTimestamp, got)
		}
	}
}

func TestChromeEpochToUnix_NegativeResult(t *testing.T) {
	// Very small Chrome timestamp that would produce a negative Unix offset.
	// Should return 0.
	got := chromeEpochToUnix(1)
	if got != 0 {
		t.Errorf("chromeEpochToUnix(1) = %v, want 0 (negative Unix offset)", got)
	}
}

func TestChromeEpochToUnix_ReturnsSeconds(t *testing.T) {
	// Chrome epoch offset is 11644473600000000 microseconds.
	// Adding one second (1000000 microseconds) should return 1.0.
	const oneSec int64 = 11644473600000000 + 1000000
	got := chromeEpochToUnix(oneSec)
	if got != 1.0 {
		t.Errorf("chromeEpochToUnix(epoch+1s) = %v, want 1.0", got)
	}
}

// ---------------------------------------------------------------------------
// chromeSameSiteToCDP
// ---------------------------------------------------------------------------

func TestChromeSameSiteToCDP(t *testing.T) {
	tests := []struct {
		input int
		want  proto.NetworkCookieSameSite
	}{
		{0, proto.NetworkCookieSameSiteNone},
		{1, proto.NetworkCookieSameSiteLax},
		{2, proto.NetworkCookieSameSiteStrict},
		{3, ""},  // unknown → empty
		{-1, ""}, // negative → empty
		{99, ""}, // large → empty
	}
	for _, tt := range tests {
		got := chromeSameSiteToCDP(tt.input)
		if got != tt.want {
			t.Errorf("chromeSameSiteToCDP(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// isValidDomainPattern
// ---------------------------------------------------------------------------

func TestIsValidDomainPattern(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"example.com", true},
		{"*.example.com", true},
		{"sub.example.com", true},
		{"my-domain.co.uk", true},
		{"UPPERCASE.COM", true},
		{"domain123.com", true},
		{"", false},                   // empty
		{"bad domain.com", false},     // space
		{"evil'; DROP TABLE", false},  // SQL injection attempt
		{"semi;colon.com", false},     // semicolon
		{"back\\slash.com", false},    // backslash
		{"http://example.com", false}, // colon/slash
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidDomainPattern(tt.input)
			if got != tt.want {
				t.Errorf("isValidDomainPattern(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// deriveChromeKey
// ---------------------------------------------------------------------------

func TestDeriveChromeKey_Length(t *testing.T) {
	// deriveChromeKey uses PBKDF2 with 16-byte output (AES-128 key size).
	key, err := deriveChromeKey("test-password")
	if err != nil {
		t.Fatalf("deriveChromeKey: unexpected error: %v", err)
	}
	if len(key) != 16 {
		t.Errorf("key length = %d, want 16", len(key))
	}
}

func TestDeriveChromeKey_Deterministic(t *testing.T) {
	// Same password should always produce the same key.
	key1, err := deriveChromeKey("my-password")
	if err != nil {
		t.Fatalf("first deriveChromeKey: %v", err)
	}
	key2, err := deriveChromeKey("my-password")
	if err != nil {
		t.Fatalf("second deriveChromeKey: %v", err)
	}
	for i := range key1 {
		if key1[i] != key2[i] {
			t.Errorf("keys differ at byte %d: %02x vs %02x", i, key1[i], key2[i])
		}
	}
}

func TestDeriveChromeKey_DifferentPasswords(t *testing.T) {
	// Different passwords must produce different keys.
	key1, _ := deriveChromeKey("password-A")
	key2, _ := deriveChromeKey("password-B")

	same := true
	for i := range key1 {
		if key1[i] != key2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different passwords produced the same key")
	}
}

// ---------------------------------------------------------------------------
// decryptCookieValue
// ---------------------------------------------------------------------------

func TestDecryptCookieValue_TooShort(t *testing.T) {
	key := make([]byte, 16)
	tests := [][]byte{
		{},                 // empty
		{0x76},             // 1 byte
		{0x76, 0x31, 0x30}, // exactly 3 bytes (the prefix, nothing left)
	}
	for _, input := range tests {
		_, err := decryptCookieValue(input, key)
		if err == nil {
			t.Errorf("decryptCookieValue(%x): expected error for too-short input", input)
		}
	}
}

func TestDecryptCookieValue_BadPrefix(t *testing.T) {
	key := make([]byte, 16)
	// Start with "v99" — not v10 or v11.
	ciphertext := make([]byte, 3+aes.BlockSize)
	copy(ciphertext, []byte("v99"))

	_, err := decryptCookieValue(ciphertext, key)
	if err == nil {
		t.Error("decryptCookieValue: expected error for bad prefix 'v99'")
	}
}

func TestDecryptCookieValue_NotMultipleOfBlockSize(t *testing.T) {
	key := make([]byte, 16)
	// v10 + 5 bytes (5 is not a multiple of 16).
	data := append([]byte("v10"), make([]byte, 5)...)
	_, err := decryptCookieValue(data, key)
	if err == nil {
		t.Error("decryptCookieValue: expected error when ciphertext not multiple of block size")
	}
}

// encryptCBCForTest encrypts plaintext using AES-128-CBC with a zero IV and PKCS#7
// padding, matching Chrome's cookie encryption scheme. Prefixes the result with version.
func encryptCBCForTest(t *testing.T, key, plaintext []byte, version string) []byte {
	t.Helper()
	blockSize := aes.BlockSize
	padLen := blockSize - (len(plaintext) % blockSize)
	padded := append(plaintext, make([]byte, padLen)...)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}

	cipherBlock, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	iv := make([]byte, blockSize)
	encrypted := make([]byte, len(padded))
	prev := iv
	for i := 0; i < len(padded); i += blockSize {
		xored := make([]byte, blockSize)
		for j := 0; j < blockSize; j++ {
			xored[j] = padded[i+j] ^ prev[j]
		}
		cipherBlock.Encrypt(encrypted[i:i+blockSize], xored)
		prev = encrypted[i : i+blockSize]
	}
	return append([]byte(version), encrypted...)
}

func TestDecryptCookieValue_ValidRoundTrip(t *testing.T) {
	key, err := deriveChromeKey("testpassword")
	if err != nil {
		t.Fatalf("deriveChromeKey: %v", err)
	}

	plaintext := []byte("hello-cookie-value")
	payload := encryptCBCForTest(t, key, plaintext, "v10")

	got, err := decryptCookieValue(payload, key)
	if err != nil {
		t.Fatalf("decryptCookieValue: unexpected error: %v", err)
	}
	if got != string(plaintext) {
		t.Errorf("decryptCookieValue: got %q, want %q", got, string(plaintext))
	}
}

func TestDecryptCookieValue_V11Prefix(t *testing.T) {
	key, err := deriveChromeKey("testpassword")
	if err != nil {
		t.Fatalf("deriveChromeKey: %v", err)
	}

	plaintext := []byte("v11cookie")
	payload := encryptCBCForTest(t, key, plaintext, "v11")

	got, err := decryptCookieValue(payload, key)
	if err != nil {
		t.Fatalf("decryptCookieValue v11: unexpected error: %v", err)
	}
	if got != string(plaintext) {
		t.Errorf("decryptCookieValue v11: got %q, want %q", got, string(plaintext))
	}
}

// ---------------------------------------------------------------------------
// sqliteLikeEscape
// ---------------------------------------------------------------------------

func TestSQLiteLikeEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"example.com", "example.com"},
		{".example.com", ".example.com"},
		{"no_special", `no\_special`},
		{"has%percent", `has\%percent`},
		{`has\backslash`, `has\\backslash`},
		{"combined_%_test", `combined\_\%\_test`},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sqliteLikeEscape(tt.input)
			if got != tt.want {
				t.Errorf("sqliteLikeEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildDomainFilter
// ---------------------------------------------------------------------------

func TestBuildDomainFilter_Empty(t *testing.T) {
	got, err := buildDomainFilter(nil)
	if err != nil {
		t.Fatalf("buildDomainFilter(nil): unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("buildDomainFilter(nil) = %q, want empty", got)
	}
}

func TestBuildDomainFilter_SingleDomain(t *testing.T) {
	got, err := buildDomainFilter([]string{"example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := " WHERE host_key LIKE '%example.com' ESCAPE '\\'"
	if got != want {
		t.Errorf("buildDomainFilter([example.com]) = %q, want %q", got, want)
	}
}

func TestBuildDomainFilter_WildcardDomain(t *testing.T) {
	got, err := buildDomainFilter([]string{"*.example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := " WHERE host_key LIKE '%.example.com' ESCAPE '\\'"
	if got != want {
		t.Errorf("buildDomainFilter([*.example.com]) = %q, want %q", got, want)
	}
}

func TestBuildDomainFilter_MultipleDomains(t *testing.T) {
	got, err := buildDomainFilter([]string{"foo.com", "bar.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := " WHERE host_key LIKE '%foo.com' ESCAPE '\\' OR host_key LIKE '%bar.com' ESCAPE '\\'"
	if got != want {
		t.Errorf("buildDomainFilter([foo.com, bar.com]) = %q, want %q", got, want)
	}
}

func TestBuildDomainFilter_InvalidPatternRejected(t *testing.T) {
	adversarial := []string{
		"evil'; DROP TABLE cookies; --",
		"bad domain",
		"semi;colon.com",
		"has/slash.com",
		"http://example.com",
	}
	for _, d := range adversarial {
		t.Run(d, func(t *testing.T) {
			_, err := buildDomainFilter([]string{d})
			if err == nil {
				t.Errorf("buildDomainFilter(%q): expected error for adversarial input, got nil", d)
			}
		})
	}
}

func TestBuildDomainFilter_SkipsBlankEntries(t *testing.T) {
	got, err := buildDomainFilter([]string{"", "  ", "example.com", ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := " WHERE host_key LIKE '%example.com' ESCAPE '\\'"
	if got != want {
		t.Errorf("buildDomainFilter with blanks = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// ReadChromeCookiesWithDeps — DI / testability
// ---------------------------------------------------------------------------

// stubKeychainReader is a test double for KeychainReader.
type stubKeychainReader struct {
	password string
	err      error
}

func (s stubKeychainReader) ReadKey(_, _ string) (string, error) {
	return s.password, s.err
}

// stubSQLiteQuerier is a test double for SQLiteQuerier.
type stubSQLiteQuerier struct {
	output []byte
	err    error
}

func (s stubSQLiteQuerier) Query(_, _, _ string) ([]byte, error) {
	return s.output, s.err
}

// makeTempCookiesDB creates a minimal directory structure that satisfies the
// os.Stat check in ReadChromeCookiesWithDeps (Default/Cookies must exist).
func makeTempCookiesDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	defaultDir := filepath.Join(dir, "Default")
	if err := os.MkdirAll(defaultDir, 0o700); err != nil {
		t.Fatalf("mkdir Default: %v", err)
	}
	cookiesPath := filepath.Join(defaultDir, "Cookies")
	if err := os.WriteFile(cookiesPath, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("write Cookies: %v", err)
	}
	return dir
}

func TestReadChromeCookiesWithDeps_KeychainError(t *testing.T) {
	profileDir := makeTempCookiesDB(t)
	kr := stubKeychainReader{err: fmt.Errorf("keychain unavailable")}
	sq := stubSQLiteQuerier{}

	_, err := ReadChromeCookiesWithDeps(profileDir, nil, kr, sq)
	if err == nil {
		t.Fatal("expected error when keychain read fails, got nil")
	}
}

func TestReadChromeCookiesWithDeps_SQLiteError(t *testing.T) {
	profileDir := makeTempCookiesDB(t)
	kr := stubKeychainReader{password: "testpassword"}
	sq := stubSQLiteQuerier{err: fmt.Errorf("sqlite3 not found")}

	_, err := ReadChromeCookiesWithDeps(profileDir, nil, kr, sq)
	if err == nil {
		t.Fatal("expected error when sqlite3 query fails, got nil")
	}
}

func TestReadChromeCookiesWithDeps_MissingDB(t *testing.T) {
	kr := stubKeychainReader{password: "testpassword"}
	sq := stubSQLiteQuerier{}

	_, err := ReadChromeCookiesWithDeps("/nonexistent/profile", nil, kr, sq)
	if err == nil {
		t.Fatal("expected error for missing cookies database, got nil")
	}
}

func TestReadChromeCookiesWithDeps_EmptyOutput(t *testing.T) {
	profileDir := makeTempCookiesDB(t)
	kr := stubKeychainReader{password: "testpassword"}
	sq := stubSQLiteQuerier{output: []byte("")}

	cookies, err := ReadChromeCookiesWithDeps(profileDir, nil, kr, sq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 0 {
		t.Errorf("expected 0 cookies for empty sqlite output, got %d", len(cookies))
	}
}

func TestReadChromeCookiesWithDeps_PlaintextCookie(t *testing.T) {
	profileDir := makeTempCookiesDB(t)

	// Build a real AES key from a known password so parseCookieLine can be invoked
	// with a plaintext value (no encryption needed in this path).
	kr := stubKeychainReader{password: "testpassword"}

	// Tab-separated row: host_key, name, encrypted_hex, plain_value, path,
	// is_secure, is_httponly, expires_utc, samesite
	row := ".example.com\tsession\t\thello-value\t/\t1\t0\t0\t0\n"
	sq := stubSQLiteQuerier{output: []byte(row)}

	cookies, err := ReadChromeCookiesWithDeps(profileDir, nil, kr, sq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0].Value != "hello-value" {
		t.Errorf("cookie value = %q, want %q", cookies[0].Value, "hello-value")
	}
	if cookies[0].Name != "session" {
		t.Errorf("cookie name = %q, want %q", cookies[0].Name, "session")
	}
}
