package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
)

type userInfoContextKey struct{}

func GetAuthInfo(ctx context.Context) string {
	v := ctx.Value(userInfoContextKey{})
	if v == nil {
		return ""
	}
	return v.(string)
}

func authenticate(_ context.Context, req *http.Request) (string, error) {
	if req.TLS == nil {
		return "", errors.New("no TLS")
	}
	if len(req.TLS.VerifiedChains) == 0 || len(req.TLS.VerifiedChains[0]) == 0 {
		return "", errors.New("no verified chains")
	}
	name := req.TLS.VerifiedChains[0][0].Subject.CommonName
	return name, nil
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		name, err := authenticate(ctx, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		slog.Info("authenticated", "user", name)
		ctx = context.WithValue(ctx, userInfoContextKey{}, name)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
