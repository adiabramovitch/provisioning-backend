package middleware

import (
	"context"
	"net/http"
	"strconv"

	"github.com/rs/zerolog"
)

type ctxKeyType int

const (
	OffsetCtxKey ctxKeyType = iota
	LimitCtxKey
	TokenCtxKey
)

// Pagination middleware is used to extract the offset and the limit
func Pagination(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := zerolog.Ctx(r.Context())
		newCtx := r.Context()
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil || offset < 0 {
			logger.Trace().Err(err).Msg("offset is invalid setting offset value to 0")
			offset = 0
		}
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit < 1 {
			logger.Trace().Err(err).Msg("limit is invalid setting limit value to 0")
			limit = 100
		}
		token := r.URL.Query().Get("token")

		if token == "" {
			logger.Trace().Err(err).Msg("token is not provided, the first page is required")
		}
		newCtx = context.WithValue(newCtx, TokenCtxKey, token)
		newCtx = context.WithValue(newCtx, OffsetCtxKey, offset)
		newCtx = context.WithValue(newCtx, LimitCtxKey, limit)
		next.ServeHTTP(w, r.WithContext(newCtx))
	})
}
