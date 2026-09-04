package service

import (
	"context"
	"sync"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/rs/zerolog/log"
)

// SubscriptionReader — доступ к подпискам пользователя.
//
// Интерфейс узкий и объявлен здесь, а не в repository: сервису нужен ровно один
// метод, а полный репозиторный интерфейс распухал бы фейками в каждом тесте.
type SubscriptionReader interface {
	ActiveByUser(ctx context.Context, userId int) ([]api.Subscription, error)
}

// PlusGrantReader — доступ к грантам: Plus, выданный решением админа.
//
// Отдельный узкий интерфейс рядом с SubscriptionReader и по той же причине:
// сервису нужен один метод, а полный репозиторный распухал бы фейками.
type PlusGrantReader interface {
	LiveByUser(ctx context.Context, userId int, now time.Time) (*api.PlusGrant, error)
}

// EntitlementsConfig — параметры резолва тарифа.
type EntitlementsConfig struct {
	// FreeQuota/PlusQuota/LegacyQuota — суточные лимиты распознавания.
	// UnlimitedQuota (-1) снимает потолок.
	FreeQuota   int
	PlusQuota   int
	LegacyQuota int
	// DeliverySlack — запас на задержку ДОСТАВКИ уведомления о продлении, а не
	// собственный grace-период. Продление и billing retry стора уже отражены в
	// ExpiresAt; добавлять к ним свои сутки значит раздавать платное бесплатно
	// и врать на экране «подписка до такого-то».
	DeliverySlack time.Duration
	// CompUserIds — тариф без покупки: владелец проекта и демо-аккаунт
	// ревьюера магазина. Ревьюеру нужен рабочий Plus, а сандбокс-покупки на
	// проде не принимаются (см. store.Environment).
	CompUserIds []int
	// CacheTTL — на сколько кешируется резолв. Тариф считается на КАЖДОМ
	// распознавании, и без кеша это лишний запрос в mongo на горячем пути.
	CacheTTL time.Duration
}

type tierCacheEntry struct {
	tier api.Tier
	at   time.Time
}

// Entitlements отвечает на единственный вопрос: платный ли этот человек.
//
// Это ЕДИНСТВЕННЫЙ источник правды о тарифе. Клиент своего тарифа не сообщает
// никогда — он присылает лишь чек стора, который проверяется отдельно.
type Entitlements struct {
	subs SubscriptionReader
	// grants — необязательная зависимость: nil означает поведение до появления
	// грантов. Сеттером, а не параметром конструктора: NewEntitlements зовут из
	// шести тестов и main.go, и третий позиционный параметр — семь правок ради
	// ничего (та же конвенция, что у сеттеров rest.Server).
	grants PlusGrantReader
	cfg    EntitlementsConfig
	comp   map[int]struct{}
	now    func() time.Time // подменяется в тестах

	mu    sync.RWMutex
	cache map[int]tierCacheEntry
}

func NewEntitlements(subs SubscriptionReader, cfg EntitlementsConfig) *Entitlements {
	comp := make(map[int]struct{}, len(cfg.CompUserIds))
	for _, id := range cfg.CompUserIds {
		comp[id] = struct{}{}
	}
	return &Entitlements{
		subs:  subs,
		cfg:   cfg,
		comp:  comp,
		now:   time.Now,
		cache: map[int]tierCacheEntry{},
	}
}

// SetGrants подключает гранты. Звать до старта обслуживания: поле читается из
// обработчиков без блокировки.
func (e *Entitlements) SetGrants(grants PlusGrantReader) {
	e.grants = grants
}

// IsComp — человек из списка в окружении (владелец, демо-аккаунт ревьюера).
//
// Нужен админскому API, чтобы отличить «платит» от «подарено» и от comp: карта
// приватная, а Server.sandboxUsers не замена — он заполняется только при живых
// ключах стора.
func (e *Entitlements) IsComp(userId int) bool {
	_, ok := e.comp[userId]
	return ok
}

// Tier возвращает тариф пользователя.
//
// Политика отказов — fail CLOSED: если подписки прочитать не удалось, человек
// считается бесплатным. Обратное («не смогли проверить — пусть будет Plus»)
// означало бы, что лежащая база раздаёт платное всем.
//
// Ошибка при этом ВОЗВРАЩАЕТСЯ, а не глотается: вызывающий сам решает, показать
// ли 500 или продолжить с бесплатным лимитом.
func (e *Entitlements) Tier(ctx context.Context, userId int) (api.Tier, error) {
	if _, ok := e.comp[userId]; ok {
		return api.TierPlus, nil
	}

	if tier, ok := e.cached(userId); ok {
		return tier, nil
	}

	subs, err := e.subs.ActiveByUser(ctx, userId)
	if err != nil {
		return api.TierFree, err
	}

	now := e.now()
	tier := api.TierFree
	for i := range subs {
		if subs[i].Active(now, e.cfg.DeliverySlack) {
			tier = api.TierPlus
			break
		}
	}

	// Гранты — ПОСЛЕДНИМИ и только если Plus ещё не найден. Две причины.
	// Первая: подписка — частый случай, ранний выход экономит запрос на горячем
	// пути (тариф считается на каждом распознавании). Вторая, важнее: при
	// отказе коллекции грантов платящий подписчик не должен разжаловаться до
	// free — до грантов дело просто не дойдёт.
	if tier == api.TierFree && e.grants != nil {
		grant, err := e.grants.LiveByUser(ctx, userId, now)
		if err != nil {
			// Тот же fail closed, что у подписок: ошибка возвращается наружу,
			// TierOrFree разжалует до free.
			return api.TierFree, err
		}
		if grant.Live(now) {
			tier = api.TierPlus
		}
	}

	e.store(userId, tier, now)
	return tier, nil
}

// QuotaFor — суточный лимит распознаваний для тарифа.
//
// knowsPaywall — умеет ли сборка показать экран оплаты (определяется по
// заголовку версии клиента). Не умеет — действует legacy-лимит: урезать такую
// сборку до бесплатных пяти значило бы сломать распознавание человеку, который
// ещё не обновился, и не дать ему при этом никакого пути заплатить.
func (e *Entitlements) QuotaFor(tier api.Tier, knowsPaywall bool) int {
	if tier == api.TierPlus {
		return e.cfg.PlusQuota
	}
	if !knowsPaywall {
		return e.cfg.LegacyQuota
	}
	return e.cfg.FreeQuota
}

// Invalidate сбрасывает кеш тарифа. Зовётся сразу после покупки и после
// обработки уведомления стора: человек, который только что заплатил, обязан
// увидеть Plus немедленно, а не через минуту.
func (e *Entitlements) Invalidate(userId int) {
	e.mu.Lock()
	delete(e.cache, userId)
	e.mu.Unlock()
}

func (e *Entitlements) cached(userId int) (api.Tier, bool) {
	if e.cfg.CacheTTL <= 0 {
		return "", false
	}
	e.mu.RLock()
	entry, ok := e.cache[userId]
	e.mu.RUnlock()
	if !ok || e.now().Sub(entry.at) > e.cfg.CacheTTL {
		return "", false
	}
	return entry.tier, true
}

func (e *Entitlements) store(userId int, tier api.Tier, at time.Time) {
	if e.cfg.CacheTTL <= 0 {
		return
	}
	e.mu.Lock()
	e.cache[userId] = tierCacheEntry{tier: tier, at: at}
	e.mu.Unlock()
}

// TierOrFree — Tier с проглоченной ошибкой, для горячего пути распознавания.
//
// Распознавание не должно падать пятисоткой из-за недоступной коллекции
// подписок: человек в худшем случае получает бесплатный лимит, а сбой уходит в
// лог. Там, где ошибку надо показать (экран управления подпиской), зовётся Tier.
func (e *Entitlements) TierOrFree(ctx context.Context, userId int) api.Tier {
	tier, err := e.Tier(ctx, userId)
	if err != nil {
		log.Error().Err(err).Int("userId", userId).Msg("cannot resolve tier, falling back to free")
		return api.TierFree
	}
	return tier
}
