package rest

import (
	"context"
	"net/http"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
)

// Коды отказа по лимиту распознавания.
//
// Их два, а не один, и это принципиально: на errCodeRateLimited клиент
// показывает спокойный тост «подождите минуту», на errCodeAiQuotaExceeded —
// экран оплаты. Пока причина была одна, человек, тыкнувший микрофон дважды
// подряд, получал бы предложение заплатить, а упёршийся в суточную норму —
// невнятное «попробуйте позже» без единого намёка, что делать дальше.
const (
	errCodeRateLimited      = "rate_limited"
	errCodeAiQuotaExceeded  = "ai_quota_exceeded"
	msgRateLimited          = "слишком часто, попробуйте через минуту"
	msgAiQuotaExceededDaily = "распознавания на сегодня закончились"
)

// clientVersionHeader — версия приложения, приславшего запрос.
//
// Заголовка НЕТ — значит сборка старше 1.7: она не знает про тарифы и не умеет
// показать экран оплаты. Такой сборке даётся legacy-лимит, иначе распознавание
// сломалось бы у всех, кто ещё не обновился, и заплатить им было бы негде.
//
// ⚠️ Это ramp совместимости, а НЕ граница безопасности: клиент может заголовок
// не слать и получить legacy-лимит вместо бесплатного. Потолок злоупотребления
// ограничен самим legacy-лимитом; после раскатки 1.7 он опускается до
// бесплатного, и ветка уходит.
const clientVersionHeader = "X-Client-Version"

// quotaDto — состояние суточной квоты для клиента.
//
// Живёт в REST-слое, а не в ai.ParseResult: тот сериализуется прямо из
// доменной структуры парсера, и квоте, понятию про тарифы и деньги, там не место.
type quotaDto struct {
	Tier      api.Tier  `json:"tier"`
	Limit     int       `json:"limit"`
	Used      int64     `json:"used"`
	Remaining int64     `json:"remaining"`
	Unlimited bool      `json:"unlimited"`
	ResetsAt  time.Time `json:"resetsAt"`
}

func toQuotaDto(tier api.Tier, dec service.Decision) quotaDto {
	return quotaDto{
		Tier:      tier,
		Limit:     dec.Limit,
		Used:      dec.Used,
		Remaining: dec.Remaining(),
		Unlimited: dec.Unlimited(),
		ResetsAt:  dec.ResetsAt,
	}
}

// knowsPaywall — умеет ли приславшая сборка показать экран оплаты.
func knowsPaywall(r *http.Request) bool {
	return r.Header.Get(clientVersionHeader) != ""
}

// tierAndQuota резолвит тариф и соответствующий ему суточный лимит.
//
// Сервер без подписок (entitlements не подключён) ведёт себя как до введения
// тарифов — безлимит. Ноль здесь означал бы «ноль распознаваний», то есть тихо
// сломал бы фичу там, где её просто не настраивали.
func (s *Server) tierAndQuota(ctx context.Context, r *http.Request, userId int) (api.Tier, int) {
	if s.entitlements == nil {
		return api.TierFree, service.UnlimitedQuota
	}
	tier := s.entitlements.TierOrFree(ctx, userId)
	return tier, s.entitlements.QuotaFor(tier, knowsPaywall(r))
}

// writeQuotaRejection отвечает на исчерпанный лимит, различая минутный троттл и
// суточную норму.
//
// Тело — канонический вложенный конверт плюс quota рядом с ним: клиенту нужен и
// код (что показать), и остаток (что написать на экране оплаты).
func writeQuotaRejection(w http.ResponseWriter, tier api.Tier, dec service.Decision) {
	if dec.Kind == service.KindDaily {
		writeErrorWithQuota(w, http.StatusTooManyRequests,
			errCodeAiQuotaExceeded, msgAiQuotaExceededDaily, toQuotaDto(tier, dec))
		return
	}
	writeErrorWithQuota(w, http.StatusTooManyRequests,
		errCodeRateLimited, msgRateLimited, toQuotaDto(tier, dec))
}

// handleGetAiQuota GET /api/v1/me/ai-quota — остаток распознаваний.
//
// Нужен на холодный старт экрана: при самом распознавании остаток приезжает в
// ответе /parse, и опрашивать его отдельно не приходится. Просмотр остатка его
// НЕ расходует.
func (s *Server) handleGetAiQuota(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userId := userIdFromCtx(ctx)

	tier, quota := s.tierAndQuota(ctx, r, userId)
	if s.rateLimiter == nil {
		// Распознавание выключено (нет ключа Gemini): лимита не существует.
		writeJSON(w, http.StatusOK, quotaDto{
			Tier:      tier,
			Limit:     service.UnlimitedQuota,
			Unlimited: true,
			ResetsAt:  time.Now().UTC(),
		})
		return
	}

	dec, err := s.rateLimiter.Peek(ctx, userId, quota)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errCodeInternal, "не удалось прочитать лимит")
		return
	}
	writeJSON(w, http.StatusOK, toQuotaDto(tier, dec))
}
