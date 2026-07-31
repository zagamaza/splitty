package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// appleTokenURL — обмен authorizationCode на токены Apple.
	appleTokenURL = "https://appleid.apple.com/auth/token"

	// appleRevokeURL — отзыв выданных токенов. Обязателен при удалении аккаунта
	// по Apple Guideline 5.1.1(v).
	appleRevokeURL = "https://appleid.apple.com/auth/revoke"

	// appleAudience — получатель client_secret: сам Apple.
	appleAudience = "https://appleid.apple.com"

	// appleClientSecretTTL — срок жизни client_secret. Apple разрешает до
	// шести месяцев, но секрет собирается на каждый запрос заново (подпись
	// ES256 стоит микросекунды), поэтому короткий срок только уменьшает цену
	// утечки логов и не требует ни кеша, ни ротации.
	appleClientSecretTTL = 5 * time.Minute

	// appleHTTPTimeout — потолок одного похода к Apple. Обмен best-effort:
	// вход не должен ждать лежащего провайдера дольше этого.
	appleHTTPTimeout = 10 * time.Second

	// maxAppleResponseBytes — внешнему хосту не доверяем, тело читаем
	// ограниченно (io.LimitReader), как и JWKS.
	maxAppleResponseBytes = 1 << 20
)

// AppleTokens — token-эндпоинты Apple, нужные жизненному циклу аккаунта:
// обмен authorizationCode на refresh token при входе и отзыв этого токена при
// удалении аккаунта.
//
// Refresh token нужен НЕ для входа: Apple Guideline 5.1.1(v) требует, чтобы
// приложение с Sign in with Apple при удалении аккаунта отзывало выданные
// токены (POST /auth/revoke), а отзывать нечего без refresh token. Получить
// его можно только обменом кода в момент входа — код одноразовый и живёт
// около пяти минут, до удаления аккаунта он не доживёт.
//
// Интерфейс объявлен здесь, чтобы хендлеры входа и удаления подставляли в
// тестах фейк и никуда не ходили по сети.
type AppleTokens interface {
	ExchangeCode(ctx context.Context, code string) (string, error)
	// RevokeToken отзывает refresh token. Вызывающий обязан считать ошибку
	// НЕфатальной: недоступность Apple не должна блокировать удаление аккаунта
	RevokeToken(ctx context.Context, refreshToken string) error
}

// AppleTokenClient — клиент token-эндпоинта Apple. Авторизуется client_secret'ом
// (ES256-JWT, подписанным ключом из .p8), который собирает сам.
type AppleTokenClient struct {
	teamID   string
	keyID    string
	clientID string
	key      *ecdsa.PrivateKey

	httpClient *http.Client
	now        func() time.Time
	// tokenURL и revokeURL переопределяются в тестах, чтобы не ходить к Apple
	tokenURL  string
	revokeURL string
}

// NewAppleTokenClient собирает клиента из данных Apple Developer: Team ID,
// идентификатор ключа (Key ID), client id (bundle id приложения) и содержимое
// файла .p8 — именно СОДЕРЖИМОЕ, а не путь: ключ приезжает переменной
// окружения и в репозиторий не попадает.
func NewAppleTokenClient(teamID, keyID, clientID, privateKeyPEM string) (*AppleTokenClient, error) {
	teamID, keyID, clientID = strings.TrimSpace(teamID), strings.TrimSpace(keyID), strings.TrimSpace(clientID)
	if teamID == "" || keyID == "" || clientID == "" {
		return nil, fmt.Errorf("oidc: для обмена токенов Apple нужны team id, key id и client id")
	}
	key, err := parseApplePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	return &AppleTokenClient{
		teamID:     teamID,
		keyID:      keyID,
		clientID:   clientID,
		key:        key,
		httpClient: &http.Client{Timeout: appleHTTPTimeout},
		now:        time.Now,
		tokenURL:   appleTokenURL,
		revokeURL:  appleRevokeURL,
	}, nil
}

// parseApplePrivateKey разбирает содержимое .p8 (PKCS8, кривая P-256).
//
// Переводы строк нормализуются: PEM многострочный, а в переменную окружения
// его часто кладут одной строкой с экранированными \n (docker-compose, .env,
// секреты CI) — без нормализации такой ключ не декодируется, и обмен молча
// выключался бы при формально заданном APPLE_PRIVATE_KEY.
func parseApplePrivateKey(privateKeyPEM string) (*ecdsa.PrivateKey, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(privateKeyPEM), `\n`, "\n")
	block, _ := pem.Decode([]byte(normalized))
	if block == nil {
		return nil, fmt.Errorf("oidc: ключ Apple не разобран как PEM (ожидается содержимое файла .p8)")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("oidc: ключ Apple не разобран как PKCS8: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("oidc: ключ Apple должен быть EC (подпись ES256), получен %T", parsed)
	}
	return key, nil
}

// ClientSecret собирает client_secret для эндпоинтов Apple: ES256-JWT, где
// iss — Team ID, sub — client id, aud — сам Apple, а kid в заголовке указывает
// на ключ .p8. Нужен и обмену кода, и отзыву токенов при удалении аккаунта.
func (c *AppleTokenClient) ClientSecret() (string, error) {
	now := c.now()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{
		Issuer:    c.teamID,
		Subject:   c.clientID,
		Audience:  jwt.ClaimStrings{appleAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(appleClientSecretTTL)),
	})
	token.Header["kid"] = c.keyID
	secret, err := token.SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("oidc: не удалось подписать client_secret Apple: %w", err)
	}
	return secret, nil
}

// ExchangeCode меняет authorizationCode на refresh token.
//
// Вызывающий обязан считать ошибку НЕфатальной: вход через Apple не должен
// падать из-за того, что недоступна машинерия отзыва токенов.
func (c *AppleTokenClient) ExchangeCode(ctx context.Context, code string) (string, error) {
	body, err := c.postForm(ctx, c.tokenURL, url.Values{
		"code":       {code},
		"grant_type": {"authorization_code"},
	})
	if err != nil {
		return "", err
	}

	var payload struct {
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("oidc: ответ Apple не разобран: %w", err)
	}
	if payload.RefreshToken == "" {
		return "", fmt.Errorf("oidc: apple не вернул refresh_token (error=%q)", payload.Error)
	}
	return payload.RefreshToken, nil
}

// RevokeToken отзывает refresh token пользователя (POST /auth/revoke).
//
// Обязателен при удалении аккаунта: Apple Guideline 5.1.1(v) требует, чтобы
// приложение с Sign in with Apple не только удаляло аккаунт у себя, но и
// отзывало выданные ему токены. Успешный ответ Apple — 200 с пустым телом.
//
// Вызывающий обязан считать ошибку НЕфатальной: удаление аккаунта не может
// зависеть от доступности Apple, иначе человек не сможет удалиться, пока
// провайдер лежит.
func (c *AppleTokenClient) RevokeToken(ctx context.Context, refreshToken string) error {
	_, err := c.postForm(ctx, c.revokeURL, url.Values{
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
	})
	return err
}

// postForm выполняет form-post к эндпоинту Apple, дописывая в форму
// client_id и свежий client_secret. Тело читается ограниченно
// (io.LimitReader): внешнему хосту не доверяем
func (c *AppleTokenClient) postForm(ctx context.Context, endpoint string, form url.Values) ([]byte, error) {
	secret, err := c.ClientSecret()
	if err != nil {
		return nil, err
	}
	form.Set("client_id", c.clientID)
	form.Set("client_secret", secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oidc: не удалось собрать запрос к Apple: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: запрос к Apple не удался: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAppleResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("oidc: не удалось прочитать ответ Apple: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: apple вернул %d на %s: %s", resp.StatusCode, endpoint, strings.TrimSpace(string(body)))
	}
	return body, nil
}
