package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

type KeyProvider interface {
	// PublicKey возвращает ключ для проверки подписи.
	PublicKey() *rsa.PublicKey
	// Issuer — ожидаемый iss claim.
	Issuer() string
}

// FileKeyProvider — простейшая реализация: ключ читается один раз при старте.
type FileKeyProvider struct {
	key    *rsa.PublicKey
	issuer string
}

func NewFileKeyProvider(path, issuer string) (*FileKeyProvider, error) {
	if path == "" {
		return nil, errors.New("empty public key path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	key, err := jwt.ParseRSAPublicKeyFromPEM(raw)
	if err != nil {
		return nil, fmt.Errorf("parse RSA public key: %w", err)
	}
	return &FileKeyProvider{key: key, issuer: issuer}, nil
}

func (p *FileKeyProvider) PublicKey() *rsa.PublicKey { return p.key }
func (p *FileKeyProvider) Issuer() string            { return p.issuer }
