package examples

// Utility functions for password hashing and data operations

import (
	"crypto/des"
	"crypto/md5"
	"crypto/sha1"
	"crypto/tls"
	"database/sql"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Database configuration
const (
	DatabasePassword = "SuperSecret123!"
	APIKey           = "secret_key_EXAMPLE_not_real_abc"
	AWSAccessKey     = "AKIAIOSFODNN7EXAMPLE"
)

// Weak cryptography - MD5 (CWE-327)
func hashPasswordMD5(password string) []byte {
	hasher := md5.New()
	hasher.Write([]byte(password))
	return hasher.Sum(nil)
}

// Weak cryptography - SHA1 (CWE-327)
func hashPasswordSHA1(password string) []byte {
	hasher := sha1.New()
	hasher.Write([]byte(password))
	return hasher.Sum(nil)
}

// Weak cryptography - DES (CWE-327)
func encryptWithDES(data []byte, key []byte) error {
	block, err := des.NewCipher(key)
	if err != nil {
		return err
	}
	_ = block
	return nil
}

// Insecure TLS configuration (CWE-295)
func makeInsecureHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
}

// Insecure TLS version (CWE-326)
func makeLegacyTLSClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS10,
			},
		},
	}
}

// Command injection (CWE-78)
func executeUserCommand(userInput string) error {
	cmd := exec.Command(userInput)
	return cmd.Run()
}

// Shell command injection (CWE-78)
func executeShellCommand(userInput string) error {
	cmd := exec.Command("sh", "-c", userInput)
	return cmd.Run()
}

// SQL injection (CWE-89)
func getUserByID(db *sql.DB, userID string) error {
	query := "SELECT * FROM users WHERE id = " + userID
	_, err := db.Query(query)
	return err
}

// Path traversal (CWE-22)
func readUserFile(userPath string) ([]byte, error) {
	return ioutil.ReadFile(userPath)
}

// Path traversal with filepath.Join (CWE-22)
func readConfigFile(userInput string) ([]byte, error) {
	path := filepath.Join("/etc/config", userInput)
	return os.ReadFile(path)
}

// HTTP client without timeout (CWE-400)
func makeHTTPClientNoTimeout() *http.Client {
	return &http.Client{
		Transport: &http.Transport{},
	}
}

// Secret logging (CWE-532)
func logSecretValue(secretValue string) {
	fmt.Printf("Secret value: %s", secretValue)
}

// Valid example with proper timeout (should NOT trigger)
func makeSecureHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}
}
