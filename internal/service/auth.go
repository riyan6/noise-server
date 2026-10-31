package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"noise-server/internal/model"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

func versionString(n uint64) string { return strconv.FormatUint(n, 10) }
func sessionHash(token string) string {
	v := sha256.Sum256([]byte(token))
	return hex.EncodeToString(v[:])
}
func (s *Service) Login(ctx context.Context, name, password string) (string, error) {
	username, hash, e := s.store.Admin(ctx)
	if e != nil {
		return "", e
	}
	e = bcrypt.CompareHashAndPassword(hash, []byte(password))
	if e != nil || name != username {
		return "", model.ErrUnauthorized
	}
	token := rand.Text() + rand.Text()
	e = s.store.PutSession(ctx, sessionHash(token), s.Now().Add(SessionDuration).UnixMilli())
	return token, e
}
func (s *Service) Authenticated(ctx context.Context, token string) (bool, error) {
	if len(token) != 52 {
		return false, nil
	}
	return s.store.HasSession(ctx, sessionHash(token), s.Now().UnixMilli())
}
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, sessionHash(token))
}
func (s *Service) Username(ctx context.Context) (string, error) {
	name, _, e := s.store.Admin(ctx)
	return name, e
}
