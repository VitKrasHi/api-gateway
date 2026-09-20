package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePublicKey(t *testing.T) string {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

	path := filepath.Join(t.TempDir(), "jwt-public.pem")
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600))
	return path
}

func TestNewFileKeyProvider_OK(t *testing.T) {
	path := writePublicKey(t)

	kp, err := NewFileKeyProvider(path, "auth-service")
	require.NoError(t, err)
	assert.NotNil(t, kp.PublicKey())
	assert.Equal(t, "auth-service", kp.Issuer())
}

func TestNewFileKeyProvider_MissingFile(t *testing.T) {
	_, err := NewFileKeyProvider("/no/such/file.pem", "auth-service")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read public key")
}

func TestNewFileKeyProvider_InvalidPEM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pem")
	require.NoError(t, os.WriteFile(path, []byte("not a pem"), 0o600))

	_, err := NewFileKeyProvider(path, "auth-service")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse RSA public key")
}

func TestNewFileKeyProvider_EmptyPath(t *testing.T) {
	_, err := NewFileKeyProvider("", "auth-service")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty public key path")
}
