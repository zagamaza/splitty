package oidc

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// jwksTTL — как долго набор ключей считается свежим. Google и Apple меняют
	// ключи раз в несколько недель и публикуют новый заранее, поэтому час — с запасом.
	jwksTTL = time.Hour

	// jwksMinRefreshInterval — минимальный интервал между походами за JWKS.
	// Нужен из-за обновления «по промаху kid»: без ограничения любой желающий
	// прислал бы поток токенов с мусорным kid и превратил бы нас в усилитель
	// запросов к провайдеру (и заодно повесил бы вход себе же).
	jwksMinRefreshInterval = 5 * time.Minute

	// jwksHTTPTimeout — таймаут похода за ключами: внешний хост может подвиснуть,
	// а на этом ожидании стоит вход пользователя.
	jwksHTTPTimeout = 10 * time.Second

	// jwksMaxBody — потолок на размер ответа. Хост внешний и недоверенный,
	// io.ReadAll без ограничения — это OOM по чужой команде.
	jwksMaxBody = 1 << 20 // 1 МБ

	// minRSAKeyBits — ключи короче считаем непригодными: и Google, и Apple
	// публикуют 2048-битные, всё что меньше — либо ошибка, либо подмена.
	minRSAKeyBits = 2048
)

// jwksCache — кеш публичных ключей провайдера, полученных по JWKS-эндпоинту.
//
// Своя реализация, а не github.com/MicahParks/keyfunc: тот написан под
// golang-jwt/jwt/v4, а мы на v5 — типы jwt.Keyfunc/jwt.Token у них разные.
// В go.mod keyfunc присутствует только как indirect-зависимость чужого модуля.
//
// Поддерживаются ТОЛЬКО RSA-ключи (kty=RSA): ни Google, ни Apple не подписывают
// ID-токены EC-ключами, поэтому ES256 сознательно не реализован.
type jwksCache struct {
	url        string
	client     *http.Client
	ttl        time.Duration
	minRefresh time.Duration
	now        func() time.Time

	// mu держится в том числе на время HTTP-запроса: параллельные входы не
	// устраивают стадо одинаковых запросов к провайдеру, а ждут один.
	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	loadedAt  time.Time // когда последний раз успешно загрузили набор
	lastFetch time.Time // когда последний раз ХОДИЛИ (в т.ч. неудачно) — база троттлинга
}

func newJWKSCache(url string) *jwksCache {
	return &jwksCache{
		url:        url,
		client:     &http.Client{Timeout: jwksHTTPTimeout},
		ttl:        jwksTTL,
		minRefresh: jwksMinRefreshInterval,
		now:        time.Now,
	}
}

// key возвращает публичный ключ по kid, при необходимости обновляя кеш.
func (c *jwksCache) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if c.keys == nil || now.Sub(c.loadedAt) >= c.ttl {
		if err := c.refreshLocked(ctx); err != nil && c.keys == nil {
			return nil, err
		}
		// если протухший кеш обновить не удалось — работаем на старых ключах:
		// они всё ещё проверяют токены, выпущенные до ротации, и это лучше,
		// чем отказать всем во входе из-за недоступности провайдера
	}

	if k, ok := c.keys[kid]; ok {
		return k, nil
	}

	// промах по kid: провайдер мог выкатить новый ключ раньше, чем истёк TTL.
	// Обновляемся, но не чаще minRefresh — см. комментарий к jwksMinRefreshInterval
	if now.Sub(c.lastFetch) >= c.minRefresh {
		if err := c.refreshLocked(ctx); err != nil {
			return nil, err
		}
		if k, ok := c.keys[kid]; ok {
			return k, nil
		}
	}

	return nil, fmt.Errorf("oidc: ключ kid=%q не найден в JWKS %s", kid, c.url)
}

// refreshLocked загружает и разбирает JWKS. Вызывается под c.mu.
func (c *jwksCache) refreshLocked(ctx context.Context) error {
	c.lastFetch = c.now() // считаем попытку даже при ошибке, иначе троттлинг не работает

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, http.NoBody)
	if err != nil {
		return fmt.Errorf("oidc: запрос JWKS %s: %w", c.url, err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("oidc: загрузка JWKS %s: %w", c.url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // читаем тело, закрытие без обработки ошибки

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: JWKS %s ответил %d", c.url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, jwksMaxBody))
	if err != nil {
		return fmt.Errorf("oidc: чтение JWKS %s: %w", c.url, err)
	}

	keys, err := parseJWKS(body)
	if err != nil {
		return fmt.Errorf("oidc: разбор JWKS %s: %w", c.url, err)
	}
	if len(keys) == 0 {
		return fmt.Errorf("oidc: в JWKS %s нет ни одного пригодного RSA-ключа", c.url)
	}

	c.keys = keys
	c.loadedAt = c.now()
	return nil
}

// keyfunc — jwt.Keyfunc поверх кеша. Ключ выбирается по kid из заголовка токена.
func (c *jwksCache) keyfunc(ctx context.Context) jwt.Keyfunc {
	return func(t *jwt.Token) (interface{}, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("oidc: в заголовке токена нет kid")
		}
		return c.key(ctx, kid)
	}
}

// jwkSet — формат ответа JWKS-эндпоинта (RFC 7517).
type jwkSet struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// parseJWKS разбирает набор ключей, оставляя только пригодные RSA-ключи для подписи.
// Непригодные записи (EC, битый base64, слабый модуль) пропускаются молча —
// один плохой ключ в наборе не должен ронять весь набор.
func parseJWKS(body []byte) (map[string]*rsa.PublicKey, error) {
	var set jwkSet
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, err
	}

	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		if k.Alg != "" && k.Alg != rsaAlg {
			continue
		}
		pub, err := rsaPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}

// rsaPublicKey собирает публичный ключ из base64url-компонент n и e.
func rsaPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nb, err := decodeB64URL(nStr)
	if err != nil {
		return nil, fmt.Errorf("модуль n: %w", err)
	}
	eb, err := decodeB64URL(eStr)
	if err != nil {
		return nil, fmt.Errorf("экспонента e: %w", err)
	}
	if len(eb) == 0 || len(eb) > 8 {
		return nil, fmt.Errorf("экспонента e неправдоподобной длины: %d байт", len(eb))
	}

	n := new(big.Int).SetBytes(nb)
	if n.BitLen() < minRSAKeyBits {
		return nil, fmt.Errorf("ключ короче %d бит: %d", minRSAKeyBits, n.BitLen())
	}
	e := new(big.Int).SetBytes(eb).Int64()
	if e < 3 || e%2 == 0 {
		return nil, fmt.Errorf("недопустимая экспонента e: %d", e)
	}
	return &rsa.PublicKey{N: n, E: int(e)}, nil
}

// decodeB64URL читает base64url и с паддингом, и без — спецификация требует
// без него, но встречаются реализации, которые его дописывают.
func decodeB64URL(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}
