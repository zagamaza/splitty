package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Все тесты работают на локально сгенерированных RSA-ключах и локальном
// httptest-сервере: наружу пакет в тестах не ходит.

const (
	testIssuer   = "https://accounts.google.com"
	testAudience = "client-ios.apps.googleusercontent.com"
)

// --- инфраструктура тестов ---

// jwksServer — подставной JWKS-эндпоинт со счётчиком обращений.
type jwksServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	body []byte
	code int
	hits int32
}

func newJWKSServer(t *testing.T, keys map[string]*rsa.PublicKey) *jwksServer {
	t.Helper()
	s := &jwksServer{code: http.StatusOK}
	s.setKeys(t, keys)
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&s.hits, 1)
		s.mu.Lock()
		body, code := s.body, s.code
		s.mu.Unlock()
		w.WriteHeader(code)
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *jwksServer) setKeys(t *testing.T, keys map[string]*rsa.PublicKey) {
	t.Helper()
	set := jwkSet{}
	for kid, pub := range keys {
		set.Keys = append(set.Keys, jwkKey{
			Kty: "RSA",
			Kid: kid,
			Use: "sig",
			Alg: rsaAlg,
			N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	body, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("маршалинг JWKS: %v", err)
	}
	s.mu.Lock()
	s.body = body
	s.mu.Unlock()
}

func (s *jwksServer) setRaw(body []byte, code int) {
	s.mu.Lock()
	s.body, s.code = body, code
	s.mu.Unlock()
}

func (s *jwksServer) count() int { return int(atomic.LoadInt32(&s.hits)) }

func genKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("генерация ключа: %v", err)
	}
	return key
}

// testVerifier — верификатор, смотрящий на подставной JWKS.
func testVerifier(url string, issuers, audiences []string) *providerVerifier {
	return &providerVerifier{jwks: newJWKSCache(url), issuers: issuers, audiences: audiences}
}

func googleLikeVerifier(url string) *providerVerifier {
	return testVerifier(url, []string{"https://accounts.google.com", "accounts.google.com"}, []string{testAudience})
}

func defaultClaims() *Claims {
	now := time.Now()
	return &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuer,
			Subject:   "sub-12345",
			Audience:  jwt.ClaimStrings{testAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
		Email: "user@example.com",
		Nonce: "nonce-abc",
	}
}

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.Claims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("подпись токена: %v", err)
	}
	return s
}

// --- тесты ---

func TestVerifyValidToken(t *testing.T) {
	key := genKey(t)
	srv := newJWKSServer(t, map[string]*rsa.PublicKey{"kid-1": &key.PublicKey})
	v := googleLikeVerifier(srv.srv.URL)

	claims, err := v.Verify(context.Background(), signRS256(t, key, "kid-1", defaultClaims()))
	if err != nil {
		t.Fatalf("валидный токен отвергнут: %v", err)
	}
	if claims.Subject != "sub-12345" {
		t.Errorf("sub = %q, ожидался sub-12345", claims.Subject)
	}
	if claims.Email != "user@example.com" {
		t.Errorf("email = %q", claims.Email)
	}
	if claims.Nonce != "nonce-abc" {
		t.Errorf("nonce = %q", claims.Nonce)
	}
	if srv.count() != 1 {
		t.Errorf("походов за JWKS = %d, ожидался 1", srv.count())
	}

	// второй токен тем же ключом — кеш, в сеть не идём
	if _, err = v.Verify(context.Background(), signRS256(t, key, "kid-1", defaultClaims())); err != nil {
		t.Fatalf("второй токен отвергнут: %v", err)
	}
	if srv.count() != 1 {
		t.Errorf("кеш не сработал: походов за JWKS = %d", srv.count())
	}
}

func TestVerifyRejects(t *testing.T) {
	key := genKey(t)
	otherKey := genKey(t)
	srv := newJWKSServer(t, map[string]*rsa.PublicKey{"kid-1": &key.PublicKey})

	expired := defaultClaims()
	expired.IssuedAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-10 * time.Minute)) // за пределами leeway

	wrongAud := defaultClaims()
	wrongAud.Audience = jwt.ClaimStrings{"attacker.apps.googleusercontent.com"}

	wrongIss := defaultClaims()
	wrongIss.Issuer = "https://accounts.evil.com"

	noExp := defaultClaims()
	noExp.ExpiresAt = nil

	noSub := defaultClaims()
	noSub.Subject = ""

	tests := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{"истёкший", func(t *testing.T) string { return signRS256(t, key, "kid-1", expired) }},
		{"чужой aud", func(t *testing.T) string { return signRS256(t, key, "kid-1", wrongAud) }},
		{"чужой iss", func(t *testing.T) string { return signRS256(t, key, "kid-1", wrongIss) }},
		{"без exp", func(t *testing.T) string { return signRS256(t, key, "kid-1", noExp) }},
		{"без sub", func(t *testing.T) string { return signRS256(t, key, "kid-1", noSub) }},
		{"подписан чужим ключом", func(t *testing.T) string { return signRS256(t, otherKey, "kid-1", defaultClaims()) }},
		{"без kid", func(t *testing.T) string { return signRS256(t, key, "", defaultClaims()) }},
		{"пустой токен", func(_ *testing.T) string { return "" }},
		{"мусор вместо токена", func(_ *testing.T) string { return "not-a-jwt" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := googleLikeVerifier(srv.srv.URL)
			if _, err := v.Verify(context.Background(), tc.token(t)); err == nil {
				t.Fatal("ожидалась ошибка, токен принят")
			}
		})
	}
}

// alg:none и HS256 — классические подделки: подпись либо отсутствует, либо
// сделана «публичным ключом как секретом». Обе обязаны отвергаться, причём
// ДО похода за ключами — иначе тот же приём становится ещё и DoS на провайдера.
func TestVerifyRejectsAlgNoneAndHS256(t *testing.T) {
	key := genKey(t)
	srv := newJWKSServer(t, map[string]*rsa.PublicKey{"kid-1": &key.PublicKey})

	payload, err := json.Marshal(defaultClaims())
	if err != nil {
		t.Fatalf("маршалинг claims: %v", err)
	}
	none := strings.Join([]string{
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT","kid":"kid-1"}`)),
		base64.RawURLEncoding.EncodeToString(payload),
		"",
	}, ".")

	hsTok := jwt.NewWithClaims(jwt.SigningMethodHS256, defaultClaims())
	hsTok.Header["kid"] = "kid-1"
	hs, err := hsTok.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("подпись HS256: %v", err)
	}

	for name, token := range map[string]string{"alg none": none, "HS256": hs} {
		t.Run(name, func(t *testing.T) {
			v := googleLikeVerifier(srv.srv.URL)
			before := srv.count()
			if _, err := v.Verify(context.Background(), token); err == nil {
				t.Fatal("ожидалась ошибка, токен принят")
			}
			if srv.count() != before {
				t.Errorf("за ключами ходили %d раз, ожидалось 0: alg проверяется до keyfunc", srv.count()-before)
			}
		})
	}
}

func TestVerifyAcceptsAnyConfiguredAudienceAndIssuer(t *testing.T) {
	key := genKey(t)
	srv := newJWKSServer(t, map[string]*rsa.PublicKey{"kid-1": &key.PublicKey})
	v := testVerifier(srv.srv.URL,
		[]string{"https://accounts.google.com", "accounts.google.com"},
		[]string{"client-ios", "client-android"})

	for _, aud := range []string{"client-ios", "client-android"} {
		for _, iss := range []string{"https://accounts.google.com", "accounts.google.com"} {
			claims := defaultClaims()
			claims.Audience = jwt.ClaimStrings{aud}
			claims.Issuer = iss
			if _, err := v.Verify(context.Background(), signRS256(t, key, "kid-1", claims)); err != nil {
				t.Errorf("aud=%s iss=%s отвергнут: %v", aud, iss, err)
			}
		}
	}
}

func TestVerifyWithoutClientIDs(t *testing.T) {
	key := genKey(t)
	srv := newJWKSServer(t, map[string]*rsa.PublicKey{"kid-1": &key.PublicKey})
	v := testVerifier(srv.srv.URL, []string{testIssuer}, nil)

	if _, err := v.Verify(context.Background(), signRS256(t, key, "kid-1", defaultClaims())); err == nil {
		t.Fatal("без client id токен принят")
	}
	if srv.count() != 0 {
		t.Errorf("без client id ходили за JWKS %d раз", srv.count())
	}
}

// Неизвестный kid обновляет кеш ровно один раз, а повторный промах в пределах
// minRefresh в сеть не идёт — иначе мусорный kid превращается в усилитель DoS.
func TestUnknownKidRefreshesOnceThenThrottles(t *testing.T) {
	key := genKey(t)
	srv := newJWKSServer(t, map[string]*rsa.PublicKey{"kid-1": &key.PublicKey})
	v := googleLikeVerifier(srv.srv.URL)

	now := time.Now()
	var mu sync.Mutex
	v.jwks.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		mu.Lock()
		now = now.Add(d)
		mu.Unlock()
	}

	// прогрев кеша валидным токеном
	if _, err := v.Verify(context.Background(), signRS256(t, key, "kid-1", defaultClaims())); err != nil {
		t.Fatalf("прогрев: %v", err)
	}
	if srv.count() != 1 {
		t.Fatalf("после прогрева походов = %d", srv.count())
	}

	// прошло больше minRefresh, но меньше TTL
	advance(jwksMinRefreshInterval + time.Minute)

	bogus := signRS256(t, key, "kid-unknown", defaultClaims())
	if _, err := v.Verify(context.Background(), bogus); err == nil {
		t.Fatal("токен с неизвестным kid принят")
	}
	if srv.count() != 2 {
		t.Fatalf("промах kid должен дать РОВНО один поход за ключами, всего походов = %d", srv.count())
	}

	// повторный промах сразу же — троттлинг, в сеть не идём
	for i := 0; i < 5; i++ {
		if _, err := v.Verify(context.Background(), bogus); err == nil {
			t.Fatal("токен с неизвестным kid принят")
		}
	}
	if srv.count() != 2 {
		t.Fatalf("троттлинг не сработал: походов = %d, ожидалось 2", srv.count())
	}

	// после окончания окна — снова одна попытка
	advance(jwksMinRefreshInterval)
	if _, err := v.Verify(context.Background(), bogus); err == nil {
		t.Fatal("токен с неизвестным kid принят")
	}
	if srv.count() != 3 {
		t.Fatalf("после окончания окна ожидался ещё один поход, всего = %d", srv.count())
	}
}

// Ротация ключей: новый kid подхватывается обновлением по промаху.
func TestRotatedKeyPickedUpOnKidMiss(t *testing.T) {
	oldKey, newKey := genKey(t), genKey(t)
	srv := newJWKSServer(t, map[string]*rsa.PublicKey{"kid-old": &oldKey.PublicKey})
	v := googleLikeVerifier(srv.srv.URL)

	now := time.Now()
	v.jwks.now = func() time.Time { return now }

	if _, err := v.Verify(context.Background(), signRS256(t, oldKey, "kid-old", defaultClaims())); err != nil {
		t.Fatalf("прогрев: %v", err)
	}

	srv.setKeys(t, map[string]*rsa.PublicKey{"kid-old": &oldKey.PublicKey, "kid-new": &newKey.PublicKey})
	now = now.Add(jwksMinRefreshInterval + time.Second)

	if _, err := v.Verify(context.Background(), signRS256(t, newKey, "kid-new", defaultClaims())); err != nil {
		t.Fatalf("токен, подписанный новым ключом, отвергнут: %v", err)
	}
	if srv.count() != 2 {
		t.Errorf("походов за JWKS = %d, ожидалось 2", srv.count())
	}
}

// Провайдер лёг, TTL истёк — продолжаем работать на старых ключах,
// вход не должен падать целиком из-за недоступности JWKS.
func TestStaleKeysUsedWhenRefreshFails(t *testing.T) {
	key := genKey(t)
	srv := newJWKSServer(t, map[string]*rsa.PublicKey{"kid-1": &key.PublicKey})
	v := googleLikeVerifier(srv.srv.URL)

	now := time.Now()
	v.jwks.now = func() time.Time { return now }

	if _, err := v.Verify(context.Background(), signRS256(t, key, "kid-1", defaultClaims())); err != nil {
		t.Fatalf("прогрев: %v", err)
	}

	srv.setRaw([]byte("gateway timeout"), http.StatusBadGateway)
	now = now.Add(jwksTTL + time.Minute)

	if _, err := v.Verify(context.Background(), signRS256(t, key, "kid-1", defaultClaims())); err != nil {
		t.Fatalf("на устаревших ключах токен должен проверяться, ошибка: %v", err)
	}
}

func TestJWKSFetchErrors(t *testing.T) {
	key := genKey(t)

	t.Run("HTTP-ошибка", func(t *testing.T) {
		srv := newJWKSServer(t, map[string]*rsa.PublicKey{"kid-1": &key.PublicKey})
		srv.setRaw([]byte("nope"), http.StatusInternalServerError)
		v := googleLikeVerifier(srv.srv.URL)
		if _, err := v.Verify(context.Background(), signRS256(t, key, "kid-1", defaultClaims())); err == nil {
			t.Fatal("ожидалась ошибка при недоступном JWKS")
		}
	})

	t.Run("пустой набор ключей", func(t *testing.T) {
		srv := newJWKSServer(t, nil)
		v := googleLikeVerifier(srv.srv.URL)
		if _, err := v.Verify(context.Background(), signRS256(t, key, "kid-1", defaultClaims())); err == nil {
			t.Fatal("ожидалась ошибка при пустом JWKS")
		}
	})

	t.Run("ответ больше лимита обрезается", func(t *testing.T) {
		// валидный ключ, но следом мусорное поле на 2 МБ: после io.LimitReader
		// тело обрывается посередине и JSON перестаёт разбираться
		srv := newJWKSServer(t, map[string]*rsa.PublicKey{"kid-1": &key.PublicKey})
		huge := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"kid-1","padding":"%s"}]}`, strings.Repeat("a", 2*jwksMaxBody))
		srv.setRaw([]byte(huge), http.StatusOK)
		v := googleLikeVerifier(srv.srv.URL)
		if _, err := v.Verify(context.Background(), signRS256(t, key, "kid-1", defaultClaims())); err == nil {
			t.Fatal("ожидалась ошибка: тело JWKS должно обрезаться на 1 МБ")
		}
	})
}

func TestParseJWKSSkipsUnusable(t *testing.T) {
	key := genKey(t)
	n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())

	body := fmt.Sprintf(`{"keys":[
		{"kty":"EC","kid":"ec","crv":"P-256","x":"aaa","y":"bbb"},
		{"kty":"RSA","kid":"enc","use":"enc","alg":"RS256","n":"%s","e":"%s"},
		{"kty":"RSA","kid":"weak","use":"sig","alg":"RS256","n":"AQAB","e":"AQAB"},
		{"kty":"RSA","kid":"broken","use":"sig","alg":"RS256","n":"!!!","e":"AQAB"},
		{"kty":"RSA","kid":"good","use":"sig","alg":"RS256","n":"%s","e":"%s"}
	]}`, n, e, n, e)

	keys, err := parseJWKS([]byte(body))
	if err != nil {
		t.Fatalf("разбор JWKS: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("пригодных ключей = %d, ожидался 1: %v", len(keys), keys)
	}
	if _, ok := keys["good"]; !ok {
		t.Errorf("не найден ключ good, есть: %v", keys)
	}
}

func TestProviderConstructors(t *testing.T) {
	g, ok := NewGoogle([]string{"a", "b"}).(*providerVerifier)
	if !ok {
		t.Fatal("NewGoogle вернул неожиданный тип")
	}
	if g.jwks.url != googleJWKSURL {
		t.Errorf("JWKS Google = %s", g.jwks.url)
	}
	if len(g.issuers) != 2 || g.issuers[0] != "https://accounts.google.com" {
		t.Errorf("издатели Google = %v", g.issuers)
	}
	if len(g.audiences) != 2 {
		t.Errorf("client id Google = %v", g.audiences)
	}

	a, ok := NewApple([]string{"com.example.app"}).(*providerVerifier)
	if !ok {
		t.Fatal("NewApple вернул неожиданный тип")
	}
	if a.jwks.url != appleJWKSURL {
		t.Errorf("JWKS Apple = %s", a.jwks.url)
	}
	if len(a.issuers) != 1 || a.issuers[0] != "https://appleid.apple.com" {
		t.Errorf("издатели Apple = %v", a.issuers)
	}

	// срез client id копируется: изменение у вызывающего не меняет верификатор
	ids := []string{"x"}
	v := NewApple(ids).(*providerVerifier)
	ids[0] = "y"
	if v.audiences[0] != "x" {
		t.Errorf("client id не скопированы: %v", v.audiences)
	}
}
