// Package store проверяет чеки магазинов и превращает их в подписки Splitor.
//
// Здесь и только здесь решается, даёт ли чек Plus. Клиент своего тарифа не
// сообщает никогда: он присылает подписанный магазином чек, а сервер проверяет
// подпись и, если нужно, спрашивает у магазина текущее состояние.
package store

import (
	"time"

	"github.com/pkg/errors"
)

// Ошибки проверки чека. Различаются, потому что снаружи ведут к разным ответам:
// на ErrReceiptInvalid — 400, на ErrForeignProduct/ErrWrongEnvironment — отказ с
// объяснением, на транспортные ошибки магазина — 502 и повтор позже.
var (
	// ErrReceiptInvalid — подпись не сходится, чек подделан или испорчен.
	ErrReceiptInvalid = errors.New("чек не проходит проверку подписи")
	// ErrForeignProduct — чек на продукт, который не входит в Splitor Plus.
	// Чек чужого приложения того же аккаунта разработчика тоже сюда.
	ErrForeignProduct = errors.New("чек выписан на другой продукт")
	// ErrWrongEnvironment — sandbox-чек на проде.
	//
	// Sandbox-подписки бесплатны и продлеваются каждые несколько минут: принять
	// такой чек в проде значит раздать вечный Plus даром. Это не «странный
	// чек», а полноценная попытка обойти оплату.
	ErrWrongEnvironment = errors.New("чек выписан в другом окружении")
	// ErrNotConfigured — ключи магазина не заданы, покупки выключены.
	ErrNotConfigured = errors.New("проверка чеков не сконфигурирована")
)

// Receipt — то, что удалось достоверно узнать из чека.
//
// Осознанно плоская структура, не зависящая от библиотеки проверки: слой выше
// не должен знать, чем именно разбирался JWS.
type Receipt struct {
	// StoreRef — ключ сопоставления с уведомлениями магазина: у Apple
	// originalTransactionId, у Google purchaseToken.
	StoreRef string
	// LinkedRef — предыдущий purchaseToken (только Google, при смене плана).
	LinkedRef string
	ProductId string
	// BindingToken — токен привязки к аккаунту, который клиент передал магазину
	// при покупке. Пусто — покупка со сборки, которая его ещё не шлёт.
	BindingToken string
	ExpiresAt    time.Time
	AutoRenew    bool
	Environment  string
	// Revoked — деньги возвращены: Plus снимается немедленно, не дожидаясь
	// ExpiresAt.
	Revoked bool
	// NeedsAck — покупка ждёт подтверждения (только Google). Неподтверждённую
	// покупку Google откатывает через трое суток и возвращает деньги.
	NeedsAck bool
	// SignedAt — когда магазин подписал этот чек. Служит отсечкой против
	// переупорядоченной доставки уведомлений.
	SignedAt time.Time
}

// productAllowed — входит ли продукт в белый список Plus.
//
// Белый список, а не «любой чек нашего bundle id»: у одного аккаунта
// разработчика может быть несколько приложений и продуктов, и чек на любой
// другой не должен включать Plus.
func productAllowed(productId string, allowed []string) bool {
	for _, p := range allowed {
		if p == productId {
			return true
		}
	}
	return false
}
