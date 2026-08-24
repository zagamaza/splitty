package api

import "time"

// Tier — тариф пользователя. Считается ТОЛЬКО на сервере: клиент присылает чек
// стора, но никогда не сообщает, что он платный (см. service.Entitlements).
type Tier string

const (
	TierFree Tier = "free"
	TierPlus Tier = "plus"
)

// Магазины, выпускающие чеки.
const (
	StoreApple  = "apple"
	StoreGoogle = "google"
)

// Окружение чека. Sandbox-подписки бесплатны и продлеваются каждые несколько
// минут, поэтому на проде они не должны давать Plus (см. STORE_ALLOWED_ENVIRONMENT).
const (
	EnvSandbox    = "Sandbox"
	EnvProduction = "Production"
)

// Состояние подтверждения покупки в Google. Неподтверждённую покупку Google
// откатывает через трое суток и возвращает деньги, поэтому подтверждение — это
// состояние с ретраем, а не одиночный вызов.
const (
	AckNotApplicable = "n/a" // apple: подтверждать нечего
	AckPending       = "pending"
	AckDone          = "done"
)

// Subscription — подписка, как её видит сервер. Живёт отдельной коллекцией, а
// не полем пользователя: состояние меняют уведомления сторов и фоновый воркер,
// искать нужно по ключу стора, а не по номеру человека.
type Subscription struct {
	UserId    int    `bson:"user_id"`
	Store     string `bson:"store"`
	ProductId string `bson:"product_id"`
	// StoreRef — ключ сопоставления с уведомлениями стора: у Apple
	// originalTransactionId, у Google purchaseToken.
	//
	// ⚠️ У Google он РОТИРУЕТСЯ: смена месяц↔год выдаёт новый токен и кладёт
	// старый в linkedPurchaseToken. Без обработки этой связи одна подписка
	// превращается в два документа, и старый продолжает держать Plus даже
	// после возврата денег по новому (см. LinkedRef и Supersede).
	StoreRef string `bson:"store_ref"`
	// LinkedRef — предыдущий purchaseToken (Google linkedPurchaseToken).
	LinkedRef string `bson:"linked_ref,omitempty"`
	// BindingToken — User.PurchaseBindingToken, приехавший в чеке
	// (appAccountToken у Apple, obfuscatedExternalAccountId у Google).
	//
	// Пусто — чек от клиента, который токен ещё не передаёт (сборки до 1.7).
	// Такие принимаются: иначе купившие на старой сборке остались бы без Plus.
	BindingToken string    `bson:"binding_token,omitempty"`
	ExpiresAt    time.Time `bson:"expires_at"`
	AutoRenew    bool      `bson:"auto_renew"`
	Environment  string    `bson:"environment"`
	AckState     string    `bson:"ack_state"`
	// SupersededAt — подписку заменил документ с новым StoreRef.
	SupersededAt *time.Time `bson:"superseded_at,omitempty"`
	// RevokedAt — возврат денег или чарджбек: Plus снимается немедленно,
	// не дожидаясь ExpiresAt.
	RevokedAt *time.Time `bson:"revoked_at,omitempty"`
	// LastNotifiedAt — signedDate последнего ПРИМЕНЁННОГО уведомления.
	//
	// Идемпотентности мало: доставка приходит не по порядку, и задержавшийся
	// EXPIRED, прилетевший после DID_RENEW, погасил бы действующую подписку.
	// Уведомление старше этой отметки не применяется.
	LastNotifiedAt time.Time `bson:"last_notified_at"`
	// CheckedAt — когда последний раз ходили за состоянием в стор.
	CheckedAt time.Time `bson:"checked_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// Active — даёт ли подписка Plus на момент now.
//
// slack — запас на задержку ДОСТАВКИ уведомления, а не собственный grace-период:
// продление и billing retry стора уже отражены в ExpiresAt, и добавлять к ним
// свои сутки значит раздавать платное бесплатно.
func (s *Subscription) Active(now time.Time, slack time.Duration) bool {
	if s == nil || s.RevokedAt != nil || s.SupersededAt != nil {
		return false
	}
	return now.Before(s.ExpiresAt.Add(slack))
}
