package middleware

import (
	"strings"

	"net/http"

	"api-gateway/internal/auth"
	apierr "api-gateway/internal/errors"
	"api-gateway/internal/transport"

	"github.com/golang-jwt/jwt/v5"
)

type AuthConfig struct {
	Provider          auth.KeyProvider
	PublicRoutes      []PublicRoute
	ProtectedPrefixes []string
}

// PublicRoute описывает маршрут, доступный без токена.
// Method = "" — любой.
type PublicRoute struct {
	Method string
	Prefix string
	Exact  bool
}

func Auth(cfg AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Не наш префикс — не наше дело, пусть роутер решает.
			if !hasAnyPrefix(r.URL.Path, cfg.ProtectedPrefixes) {
				next.ServeHTTP(w, r)
				return
			}

			if isPublic(cfg.PublicRoutes, r) {
				next.ServeHTTP(w, r)
				return
			}

			token, err := extractBearer(r)
			if err != nil {
				apierr.Unauthorized(w, err.Error())
				return
			}

			claims, err := parseAndValidate(token, cfg.Provider)
			if err != nil {
				apierr.Unauthorized(w, err.Error())
				return
			}

			sub, _ := claims["sub"].(string)
			roles := extractRoles(claims)

			next.ServeHTTP(w, r.WithContext(transport.WithUser(r.Context(), sub, roles)))
		})
	}
}

func hasAnyPrefix(path string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true // по умолчанию защищаем всё
	}
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func isPublic(routes []PublicRoute, r *http.Request) bool {
	for _, pr := range routes {
		if pr.Method != "" && pr.Method != r.Method {
			continue
		}
		if pr.Exact {
			if r.URL.Path == pr.Prefix {
				return true
			}
			continue
		}
		if strings.HasPrefix(r.URL.Path, pr.Prefix) {
			return true
		}
	}
	return false
}

func extractBearer(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", errMsg("missing Authorization header")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", errMsg("invalid Authorization scheme")
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", errMsg("empty bearer token")
	}
	return tok, nil
}

func parseAndValidate(raw string, kp auth.KeyProvider) (jwt.MapClaims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(kp.Issuer()),
		jwt.WithExpirationRequired(),
	)
	token, err := parser.Parse(raw, func(t *jwt.Token) (any, error) {
		return kp.PublicKey(), nil
	})
	if err != nil {
		return nil, errMsg("invalid token: " + err.Error())
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errMsg("invalid claims type")
	}
	return claims, nil
}

func extractRoles(claims jwt.MapClaims) []string {
	raw, ok := claims["roles"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		return strings.Split(v, ",")
	default:
		return nil
	}
}

type errMsg string

func (e errMsg) Error() string { return string(e) }
