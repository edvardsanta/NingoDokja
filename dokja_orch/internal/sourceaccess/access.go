// Package sourceaccess carries authenticated Desktop operator authority only in
// native context. Event identity and arbitrary context maps grant no authority.
package sourceaccess

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
)

type principalKey struct{}
type Principal struct{ Scope, Actor string }

func ValidToken(token string) bool {
	return len(token) >= 32 && len(token) <= 512 && !strings.HasPrefix(token, "CHANGE_ME") && strings.TrimSpace(token) == token
}
func Authenticate(ctx context.Context, configured, supplied string) (context.Context, error) {
	if !ValidToken(configured) || subtle.ConstantTimeCompare([]byte(configured), []byte(supplied)) != 1 {
		return ctx, errors.New("source supervision access denied or not configured")
	}
	return context.WithValue(ctx, principalKey{}, Principal{Scope: "desktop:operator", Actor: "desktop:operator"}), nil
}
func Current(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
