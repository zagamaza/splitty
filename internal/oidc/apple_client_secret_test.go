package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// newTestApplePEM — локально сгенерированный EC-ключ в том же виде, в каком
// Apple отдаёт .p8: PKCS8 в PEM. Сеть и настоящий ключ Apple не нужны
func newTestApplePEM(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("cannot generate ec key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("cannot marshal pkcs8: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), key
}

func newTestAppleClient(t *testing.T) (*AppleTokenClient, *ecdsa.PrivateKey) {
	t.Helper()
	pemKey, key := newTestApplePEM(t)
	c, err := NewAppleTokenClient("TEAM123456", "KEY7890AB", "dev.zagirnur.splitty", pemKey)
	if err != nil {
		t.Fatalf("NewAppleTokenClient: %v", err)
	}
	return c, key
}

// client_secret — это ES256-JWT с iss=TeamID, sub=client id, aud=Apple и kid в
// заголовке. Ошибка в любом из полей означает 400 invalid_client от Apple, а
// значит — не сохранённый refresh token и невозможность отозвать токены при
// удалении аккаунта
func TestAppleClientSecretClaims(t *testing.T) {
	c, key := newTestAppleClient(t)

	secret, err := c.ClientSecret()
	if err != nil {
		t.Fatalf("ClientSecret: %v", err)
	}

	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(secret, claims, func(_ *jwt.Token) (interface{}, error) {
		return &key.PublicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}), jwt.WithAudience(appleAudience), jwt.WithIssuer("TEAM123456"),
		jwt.WithExpirationRequired())
	if err != nil {
		t.Fatalf("подпись client_secret не проверилась: %v", err)
	}
	if kid, _ := token.Header["kid"].(string); kid != "KEY7890AB" {
		t.Errorf("kid = %q, want KEY7890AB: без него Apple не найдёт ключ подписи", kid)
	}
	if claims.Subject != "dev.zagirnur.splitty" {
		t.Errorf("sub = %q, want client id приложения", claims.Subject)
	}
	if claims.ExpiresAt == nil || claims.IssuedAt == nil {
		t.Fatalf("client_secret без iat/exp: %+v", claims)
	}
	// потолок Apple — шесть месяцев; более долгий секрет отвергается
	if ttl := claims.ExpiresAt.Sub(claims.IssuedAt.Time); ttl <= 0 || ttl > 180*24*time.Hour {
		t.Errorf("ttl client_secret = %s, ожидался положительный и не больше шести месяцев", ttl)
	}
}

// Ключ приезжает переменной окружения и часто одной строкой с экранированными
// \n — такой PEM обязан разбираться, иначе обмен молча выключен при формально
// заданном APPLE_PRIVATE_KEY
func TestAppleClientAcceptsEscapedNewlines(t *testing.T) {
	pemKey, _ := newTestApplePEM(t)
	escaped := strings.ReplaceAll(pemKey, "\n", `\n`)

	if _, err := NewAppleTokenClient("TEAM123456", "KEY7890AB", "dev.zagirnur.splitty", escaped); err != nil {
		t.Fatalf("ключ с экранированными переводами строк не принят: %v", err)
	}
}

func TestNewAppleTokenClientRejectsBadInput(t *testing.T) {
	pemKey, _ := newTestApplePEM(t)

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("cannot generate rsa key: %v", err)
	}
	rsaDer, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatalf("cannot marshal rsa pkcs8: %v", err)
	}
	rsaPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rsaDer}))

	tests := []struct {
		name                        string
		teamID, keyID, clientID, pk string
	}{
		{name: "без team id", keyID: "KEY", clientID: "app", pk: pemKey},
		{name: "без key id", teamID: "TEAM", clientID: "app", pk: pemKey},
		{name: "без client id", teamID: "TEAM", keyID: "KEY", pk: pemKey},
		{name: "пустой ключ", teamID: "TEAM", keyID: "KEY", clientID: "app", pk: ""},
		{name: "не PEM", teamID: "TEAM", keyID: "KEY", clientID: "app", pk: "-----BEGIN PRIVATE KEY-----"},
		{name: "RSA вместо EC", teamID: "TEAM", keyID: "KEY", clientID: "app", pk: rsaPEM},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewAppleTokenClient(tt.teamID, tt.keyID, tt.clientID, tt.pk); err == nil {
				t.Error("ожидалась ошибка конфигурации, получен рабочий клиент")
			}
		})
	}
}

// Обмен кода: форма ровно та, что ждёт Apple, а из ответа берётся refresh_token
func TestAppleExchangeCode(t *testing.T) {
	c, key := newTestAppleClient(t)

	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("cannot parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r-1","token_type":"Bearer"}`))
	}))
	defer srv.Close()
	c.tokenURL = srv.URL

	refresh, err := c.ExchangeCode(context.Background(), "code-1")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if refresh != "r-1" {
		t.Errorf("refresh token = %q, want r-1", refresh)
	}
	if got := gotForm.Get("grant_type"); got != "authorization_code" {
		t.Errorf("grant_type = %q, want authorization_code", got)
	}
	if got := gotForm.Get("code"); got != "code-1" {
		t.Errorf("code = %q, want code-1", got)
	}
	if got := gotForm.Get("client_id"); got != "dev.zagirnur.splitty" {
		t.Errorf("client_id = %q, want client id приложения", got)
	}
	// client_secret в форме — тот же подписанный JWT, а не голый ключ
	secret := gotForm.Get("client_secret")
	if _, err := jwt.ParseWithClaims(secret, &jwt.RegisteredClaims{}, func(_ *jwt.Token) (interface{}, error) {
		return &key.PublicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"})); err != nil {
		t.Errorf("client_secret в форме не проверился: %v", err)
	}
}

func TestAppleExchangeCodeErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "ошибка apple", status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`},
		{name: "200 без refresh_token", status: http.StatusOK, body: `{"access_token":"a"}`},
		{name: "не json", status: http.StatusOK, body: `<html>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestAppleClient(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			c.tokenURL = srv.URL

			if _, err := c.ExchangeCode(context.Background(), "code-1"); err == nil {
				t.Error("ожидалась ошибка обмена")
			}
		})
	}
}

// Отзыв токенов: форма ровно та, что ждёт Apple (Guideline 5.1.1(v)).
// Успешный ответ Apple — 200 с ПУСТЫМ телом, поэтому парсить его нельзя
func TestAppleRevokeToken(t *testing.T) {
	c, key := newTestAppleClient(t)

	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("cannot parse form: %v", err)
		}
		gotForm = r.PostForm
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c.revokeURL = srv.URL

	if err := c.RevokeToken(context.Background(), "r-1"); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if got := gotForm.Get("token"); got != "r-1" {
		t.Errorf("token = %q, want r-1", got)
	}
	if got := gotForm.Get("token_type_hint"); got != "refresh_token" {
		t.Errorf("token_type_hint = %q, want refresh_token", got)
	}
	if got := gotForm.Get("client_id"); got != "dev.zagirnur.splitty" {
		t.Errorf("client_id = %q, want client id приложения", got)
	}
	secret := gotForm.Get("client_secret")
	if _, err := jwt.ParseWithClaims(secret, &jwt.RegisteredClaims{}, func(_ *jwt.Token) (interface{}, error) {
		return &key.PublicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"})); err != nil {
		t.Errorf("client_secret в форме не проверился: %v", err)
	}
}

// Отказ Apple обязан быть видимой ошибкой: вызывающий (удаление аккаунта)
// решает сам, что она нефатальна, но молча считать отзыв успешным нельзя
func TestAppleRevokeTokenError(t *testing.T) {
	c, _ := newTestAppleClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer srv.Close()
	c.revokeURL = srv.URL

	if err := c.RevokeToken(context.Background(), "r-1"); err == nil {
		t.Error("ожидалась ошибка отзыва")
	}
}
