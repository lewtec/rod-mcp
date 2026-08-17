package types

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/go-rod/rod/lib/proto"
)

// Chrome cookie encryption constants.
// Reference: https://chromium.googlesource.com/chromium/src/+/main/components/os_crypt/
const (
	// chromePBKDF2Salt is the fixed salt used by Chrome's PBKDF2 key derivation.
	chromePBKDF2Salt = "saltysalt"
	// chromePBKDF2Iterations is the number of PBKDF2 iterations Chrome uses.
	chromePBKDF2Iterations = 1003
	// chromePBKDF2KeyLen is the AES key length in bytes (AES-128).
	chromePBKDF2KeyLen = 16
	// chromeEncryptionV10 is the encryption version prefix used by Chrome on Linux.
	chromeEncryptionV10 = "v10"
	// chromeEncryptionV11 is the encryption version prefix used by Chrome on macOS.
	chromeEncryptionV11 = "v11"
)

// chromeEpochToUnix converts Chrome's expires_utc (microseconds since 1601-01-01)
// to Unix epoch seconds. Returns 0 for session cookies.
func chromeEpochToUnix(chromeTimestamp int64) float64 {
	if chromeTimestamp == 0 {
		return 0
	}
	// Chrome epoch starts at 1601-01-01. Offset to Unix epoch (1970-01-01) in microseconds.
	const chromeToUnixOffsetMicroseconds = 11644473600000000
	unixMicro := chromeTimestamp - chromeToUnixOffsetMicroseconds
	if unixMicro <= 0 {
		return 0
	}
	return float64(unixMicro) / 1000000.0
}

// chromeSameSiteToCDP converts Chrome's samesite integer to CDP enum.
func chromeSameSiteToCDP(ss int) proto.NetworkCookieSameSite {
	switch ss {
	case 0:
		return proto.NetworkCookieSameSiteNone
	case 1:
		return proto.NetworkCookieSameSiteLax
	case 2:
		return proto.NetworkCookieSameSiteStrict
	default:
		return ""
	}
}

// KeychainReader reads a secret from the OS keychain.
// The default implementation calls the macOS `security` CLI.
type KeychainReader interface {
	ReadKey(service, account string) (string, error)
}

// SQLiteQuerier executes a read-only query against an SQLite database file and
// returns the raw tab-separated output. The default implementation delegates to
// the sqlite3 CLI binary.
type SQLiteQuerier interface {
	Query(dbPath, separator, query string) ([]byte, error)
}

// defaultKeychainReader is the production KeychainReader that calls the macOS
// `security find-generic-password` command.
type defaultKeychainReader struct{}

func (defaultKeychainReader) ReadKey(service, account string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("cookie decryption is currently only supported on macOS")
	}
	cmd := exec.Command("security", "find-generic-password", "-w", "-s", service, "-a", account)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to read %s from Keychain for account %s: %w (you may need to allow access)", service, account, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// defaultSQLiteQuerier is the production SQLiteQuerier that calls the sqlite3 CLI.
type defaultSQLiteQuerier struct{}

func (defaultSQLiteQuerier) Query(dbPath, separator, query string) ([]byte, error) {
	sqlite3Path, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, fmt.Errorf("sqlite3 not found in PATH")
	}
	cmd := exec.Command(sqlite3Path, "-separator", separator, dbPath, query)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 query failed: %w", err)
	}
	return out, nil
}

// readChromeEncryptionKey reads the Chrome Safe Storage password from the macOS Keychain.
func readChromeEncryptionKey() (string, error) {
	return defaultKeychainReader{}.ReadKey("Chrome Safe Storage", "Chrome")
}

// deriveChromeKey derives the AES-128-CBC key from the Keychain password using PBKDF2.
func deriveChromeKey(password string) ([]byte, error) {
	return pbkdf2.Key(sha1.New, password, []byte(chromePBKDF2Salt), chromePBKDF2Iterations, chromePBKDF2KeyLen)
}

// decryptCookieValue decrypts a Chrome encrypted cookie value.
// Chrome prefixes encrypted values with "v10" (3 bytes), followed by AES-128-CBC ciphertext
// with a 16-byte zero IV.
func decryptCookieValue(encrypted []byte, key []byte) (string, error) {
	if len(encrypted) <= 3 {
		return "", fmt.Errorf("encrypted value too short (%d bytes)", len(encrypted))
	}

	// Strip the version prefix (v10 on Linux, v11 on macOS).
	prefix := string(encrypted[:3])
	if prefix != chromeEncryptionV10 && prefix != chromeEncryptionV11 {
		return "", fmt.Errorf("unexpected encryption prefix: %q", prefix)
	}
	ciphertext := encrypted[3:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext length %d is not a multiple of block size", len(ciphertext))
	}

	iv := make([]byte, aes.BlockSize) // 16 zero bytes
	mode := cipher.NewCBCDecrypter(block, iv)

	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// Remove PKCS#7 padding.
	if len(plaintext) == 0 {
		return "", nil
	}
	padLen := int(plaintext[len(plaintext)-1])
	if padLen > aes.BlockSize || padLen > len(plaintext) || padLen == 0 {
		return "", fmt.Errorf("invalid PKCS7 padding: %d", padLen)
	}
	plaintext = plaintext[:len(plaintext)-padLen]

	return string(plaintext), nil
}

// buildDomainFilter constructs an SQL WHERE clause for the given domain patterns.
// Wildcard patterns (*.example.com) match any subdomain; plain patterns match as a suffix.
// Returns an error if any pattern is invalid, and an empty string if domains is empty.
//
// Defense-in-depth: isValidDomainPattern rejects all SQL metacharacters before this
// function is called. As a second layer, sqliteLikeEscape escapes LIKE metacharacters
// ('_' and '%') within the domain suffix so no user-controlled value is ever
// interpolated raw into the query string.
func buildDomainFilter(domains []string) (string, error) {
	if len(domains) == 0 {
		return "", nil
	}
	var conditions []string
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if !isValidDomainPattern(d) {
			return "", fmt.Errorf("invalid domain pattern %q", d)
		}
		// Strip the leading '*' for wildcard patterns (*.example.com → .example.com).
		suffix := d
		if strings.HasPrefix(d, "*.") {
			suffix = d[1:]
		}
		// Escape LIKE metacharacters within the domain suffix as a second layer of
		// defense. The domain allowlist already forbids '%' and '_', so this is
		// belt-and-suspenders, not the primary guard.
		escaped := sqliteLikeEscape(suffix)
		conditions = append(conditions, fmt.Sprintf("host_key LIKE '%%%s' ESCAPE '\\'", escaped))
	}
	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " OR "), nil
}

// sqliteLikeEscape escapes SQLite LIKE pattern metacharacters ('%' and '_') in s
// using a backslash escape. The returned string is safe to embed inside a
// LIKE '...' ESCAPE '\\' clause.
func sqliteLikeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// parseCookieLine parses a single tab-separated cookie row from sqlite3 output
// and returns a CDP-compatible cookie param. The key is used to decrypt the
// hex-encoded encrypted_value field when no plain-text value is present.
// Returns nil without error if the cookie has no usable value.
func parseCookieLine(line string, key []byte) (*proto.NetworkCookieParam, error) {
	fields := strings.SplitN(line, "\t", 9)
	if len(fields) < 9 {
		return nil, nil
	}

	hostKey := fields[0]
	name := fields[1]
	encryptedHex := fields[2]
	plainValue := fields[3]
	path := fields[4]
	isSecure := fields[5] == "1"
	isHTTPOnly := fields[6] == "1"
	expiresUTC, _ := strconv.ParseInt(fields[7], 10, 64)
	sameSite, _ := strconv.Atoi(fields[8])

	var value string
	if plainValue != "" {
		value = plainValue
	} else if encryptedHex != "" {
		encBytes, err := hex.DecodeString(encryptedHex)
		if err != nil {
			return nil, fmt.Errorf("decode hex: %w", err)
		}
		decrypted, err := decryptCookieValue(encBytes, key)
		if err != nil {
			return nil, fmt.Errorf("decrypt: %w", err)
		}
		value = decrypted
	}

	if value == "" {
		return nil, nil
	}

	cookie := &proto.NetworkCookieParam{
		Name:     name,
		Value:    value,
		Domain:   hostKey,
		Path:     path,
		Secure:   isSecure,
		HTTPOnly: isHTTPOnly,
		SameSite: chromeSameSiteToCDP(sameSite),
	}
	if expiresUTC > 0 {
		cookie.Expires = proto.TimeSinceEpoch(chromeEpochToUnix(expiresUTC))
	}
	return cookie, nil
}

// ReadChromeCookies reads and decrypts cookies from a Chrome profile's Cookies SQLite DB.
// If domains is non-empty, only cookies matching those domains are returned.
// Returns CDP-compatible cookie params ready for injection.
// Uses the default production implementations of KeychainReader and SQLiteQuerier.
func ReadChromeCookies(profileDir string, domains []string) ([]*proto.NetworkCookieParam, error) {
	return ReadChromeCookiesWithDeps(profileDir, domains, defaultKeychainReader{}, defaultSQLiteQuerier{})
}

// ReadChromeCookiesWithDeps is the injectable variant of ReadChromeCookies.
// Callers may supply custom KeychainReader and SQLiteQuerier implementations for
// testing or alternative OS integrations.
func ReadChromeCookiesWithDeps(profileDir string, domains []string, kr KeychainReader, sq SQLiteQuerier) ([]*proto.NetworkCookieParam, error) {
	cookiesDB := filepath.Join(profileDir, "Default", "Cookies")
	if _, err := os.Stat(cookiesDB); os.IsNotExist(err) {
		return nil, fmt.Errorf("cookies database not found at %s", cookiesDB)
	}

	password, err := kr.ReadKey("Chrome Safe Storage", "Chrome")
	if err != nil {
		return nil, err
	}
	key, err := deriveChromeKey(password)
	if err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}

	whereClause, err := buildDomainFilter(domains)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(
		"SELECT host_key, name, hex(encrypted_value), value, path, is_secure, is_httponly, expires_utc, samesite FROM cookies%s;",
		whereClause,
	)
	out, err := sq.Query(cookiesDB, "\t", query)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		slog.Warn(fmt.Sprintf("no cookies found matching domains: %v", domains))
		return nil, nil
	}

	cookies := make([]*proto.NetworkCookieParam, 0, len(lines))
	var decryptErrors int
	for _, line := range lines {
		cookie, err := parseCookieLine(line, key)
		if err != nil {
			decryptErrors++
			continue
		}
		if cookie != nil {
			cookies = append(cookies, cookie)
		}
	}

	if decryptErrors > 0 {
		slog.Warn(fmt.Sprintf("failed to decrypt %d cookies (skipped)", decryptErrors))
	}
	slog.Info(fmt.Sprintf("read %d cookies from Chrome profile", len(cookies)))
	return cookies, nil
}
