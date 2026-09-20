package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"api-gateway/internal/transport"
)

// ---------- helpers ----------

type staticKeyProvider struct {
	key    *rsa.PublicKey
	issuer string
}

func (s *staticKeyProvider) PublicKey() *rsa.PublicKey { return s.key }
func (s *staticKeyProvider) Issuer() string            { return s.issuer }

func genKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return k
}

func signRS256(t *testing.T, priv *rsa.PrivateKey, issuer, sub string, roles []string, ttl time.Duration) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":   issuer,
		"sub":   sub,
		"roles": roles,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(ttl).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(priv)
	require.NoError(t, err)
	return s
}

func newAuthHandler(t *testing.T, provider *staticKeyProvider, public []PublicRoute) http.Handler {
	t.Helper()
	return Auth(AuthConfig{Provider: provider, PublicRoutes: public})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid := transport.UserID(r.Context())
			roles := transport.UserRoles(r.Context())
			w.Header().Set("X-User-ID", uid)
			w.Header().Set("X-Roles", join(roles))
			w.WriteHeader(http.StatusOK)
		}),
	)
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

// ---------- tests ----------

func TestAuth_PublicRoute_NoToken(t *testing.T) {
	priv := genKey(t)
	provider := &staticKeyProvider{key: &priv.PublicKey, issuer: "auth"}
	h := newAuthHandler(t, provider, []PublicRoute{
		{Method: http.MethodPost, Prefix: "/api/v1/auth/login", Exact: true},
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestAuth_ProtectedRoute_NoHeader(t *testing.T) {
	priv := genKey(t)
	provider := &staticKeyProvider{key: &priv.PublicKey, issuer: "auth"}
	h := newAuthHandler(t, provider, nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "missing Authorization header")
}

func TestAuth_InvalidScheme(t *testing.T) {
	priv := genKey(t)
	provider := &staticKeyProvider{key: &priv.PublicKey, issuer: "auth"}
	h := newAuthHandler(t, provider, nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid Authorization scheme")
}

func TestAuth_ValidToken_SetsUserInContext(t *testing.T) {
	priv := genKey(t)
	provider := &staticKeyProvider{key: &priv.PublicKey, issuer: "auth"}
	h := newAuthHandler(t, provider, nil)

	token := signRS256(t, priv, "auth", "user-42", []string{"user", "admin"}, time.Hour)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "user-42", rr.Header().Get("X-User-ID"))
	assert.Equal(t, "user,admin", rr.Header().Get("X-Roles"))
}

func TestAuth_ExpiredToken(t *testing.T) {
	priv := genKey(t)
	provider := &staticKeyProvider{key: &priv.PublicKey, issuer: "auth"}
	h := newAuthHandler(t, provider, nil)

	// exp = -1 час
	token := signRS256(t, priv, "auth", "user-1", nil, -time.Hour)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid token")
}

func TestAuth_WrongIssuer(t *testing.T) {
	priv := genKey(t)
	provider := &staticKeyProvider{key: &priv.PublicKey, issuer: "auth"}
	h := newAuthHandler(t, provider, nil)

	token := signRS256(t, priv, "evil-issuer", "user-1", nil, time.Hour)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuth_TamperedToken(t *testing.T) {
	priv := genKey(t)
	otherKey := genKey(t) // другой ключ
	provider := &staticKeyProvider{key: &priv.PublicKey, issuer: "auth"}
	h := newAuthHandler(t, provider, nil)

	token := signRS256(t, otherKey, "auth", "user-1", nil, time.Hour)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuth_HS256Rejected(t *testing.T) {
	priv := genKey(t)
	provider := &staticKeyProvider{key: &priv.PublicKey, issuer: "auth"}
	h := newAuthHandler(t, provider, nil)

	// Алгоритм HS256, подписан "секретом" — атака algorithm confusion.
	claims := jwt.MapClaims{
		"iss": "auth", "sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, _ := tok.SignedString([]byte("shared-secret"))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuth_PublicRoute_MethodMismatch(t *testing.T) {
	priv := genKey(t)
	provider := &staticKeyProvider{key: &priv.PublicKey, issuer: "auth"}
	h := newAuthHandler(t, provider, []PublicRoute{
		{Method: http.MethodGet, Prefix: "/api/v1/catalog/products"},
	})

	// POST на публичный GET-роут — не должен считаться публичным.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/catalog/products", nil)
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestExtractRoles(t *testing.T) {
	cases := []struct {
		name   string
		claims jwt.MapClaims
		want   []string
	}{
		{"absent", jwt.MapClaims{}, nil},
		{"strings slice", jwt.MapClaims{"roles": []string{"a", "b"}}, []string{"a", "b"}},
		{"any slice", jwt.MapClaims{"roles": []any{"a", "b"}}, []string{"a", "b"}},
		{"any slice mixed", jwt.MapClaims{"roles": []any{"a", 1, "b"}}, []string{"a", "b"}},
		{"string csv", jwt.MapClaims{"roles": "a,b,c"}, []string{"a", "b", "c"}},
		{"wrong type", jwt.MapClaims{"roles": 42}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, extractRoles(c.claims))
		})
	}
}
