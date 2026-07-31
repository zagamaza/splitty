// Package oidc проверяет ID-токены внешних провайдеров входа (Google, Apple):
// подпись по публичным ключам провайдера из JWKS, издателя (iss), получателя
// (aud) и срок действия. Разбор и проверка самого JWT — библиотечные
// (github.com/golang-jwt/jwt/v5), своими руками написана только загрузка JWKS.
package oidc

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// rsaAlg — единственный допустимый алгоритм подписи. Google и Apple
	// подписывают ID-токены RS256; всё остальное (включая none и HS256,
	// которым подделывают токены при слабой проверке) отвергается парсером.
	rsaAlg = "RS256"

	// clockLeeway — допуск на расхождение часов с провайдером.
	clockLeeway = 5 * time.Minute

	googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"
	appleJWKSURL  = "https://appleid.apple.com/auth/keys"
)

// Claims — то, что нам нужно из ID-токена.
//
// Subject (claim sub) — единственный стабильный идентификатор пользователя у
// провайдера; именно он ложится в google_sub/apple_sub. Email не идентификатор:
// его можно сменить, а Apple вообще отдаёт relay-адрес и только при первом входе.
// Nonce нужен Apple-потоку (Task 11): клиент присылает сырой nonce, в токене
// лежит его хеш.
type Claims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Nonce string `json:"nonce"`
	// Name — отображаемое имя из профиля. Есть у Google (при scope profile) и
	// нет у Apple: Apple отдаёт имя отдельным полем на клиенте и только при
	// первом входе. Используется лишь при СОЗДАНИИ пользователя — переименовать
	// себя в Splitty провайдер права не имеет.
	Name string `json:"name"`
}

// Verifier проверяет ID-токен провайдера. Интерфейс, а не структура, чтобы в
// тестах хендлеров входа подставлялся фейк без сети.
type Verifier interface {
	Verify(ctx context.Context, idToken string) (*Claims, error)
}

// providerVerifier — реализация Verifier поверх JWKS одного провайдера.
type providerVerifier struct {
	jwks *jwksCache
	// issuers — допустимые значения iss. Их может быть несколько (у Google —
	// два исторических варианта), поэтому проверка ниже своя: jwt.WithIssuer
	// принимает ровно одно значение.
	issuers []string
	// audiences — client id приложений, которым выпущен токен (iOS, Android, web).
	audiences []string
}

// NewGoogle — верификатор ID-токенов Google для перечисленных client id.
func NewGoogle(clientIDs []string) Verifier {
	return &providerVerifier{
		jwks:      newJWKSCache(googleJWKSURL),
		issuers:   []string{"https://accounts.google.com", "accounts.google.com"},
		audiences: slices.Clone(clientIDs),
	}
}

// NewApple — верификатор ID-токенов Sign in with Apple для перечисленных client id
// (bundle id приложения и/или Services ID).
func NewApple(clientIDs []string) Verifier {
	return &providerVerifier{
		jwks:      newJWKSCache(appleJWKSURL),
		issuers:   []string{"https://appleid.apple.com"},
		audiences: slices.Clone(clientIDs),
	}
}

// Verify проверяет подпись и claims токена и возвращает разобранные claims.
func (v *providerVerifier) Verify(ctx context.Context, idToken string) (*Claims, error) {
	if len(v.audiences) == 0 {
		return nil, fmt.Errorf("oidc: не задан ни один client id")
	}
	if idToken == "" {
		return nil, fmt.Errorf("oidc: пустой токен")
	}

	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{rsaAlg}),
		jwt.WithAudience(v.audiences...), // достаточно совпадения с любым из client id
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockLeeway),
	}
	// jwt.WithIssuer здесь намеренно НЕ используется: он принимает ровно одно
	// значение, а у Google их два. Проверка ниже (slices.Contains) строго
	// сильнее и покрывает оба случая — дублировать её для однозначных
	// провайдеров незачем

	claims := &Claims{}
	if _, err := jwt.ParseWithClaims(idToken, claims, v.jwks.keyfunc(ctx), opts...); err != nil {
		return nil, fmt.Errorf("oidc: токен не прошёл проверку: %w", err)
	}

	// iss при нескольких допустимых значениях проверяем сами (см. поле issuers)
	if !slices.Contains(v.issuers, claims.Issuer) {
		return nil, fmt.Errorf("oidc: неожиданный издатель токена: %q", claims.Issuer)
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("oidc: в токене нет sub")
	}
	return claims, nil
}
