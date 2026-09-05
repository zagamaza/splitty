package rest

import (
	"net/http"
	"strings"
	"testing"
)

// Публичные страницы политики и удаления аккаунта.
//
// Оба магазина требуют ссылку, открываемую БЕЗ входа: ревьюер смотрит её в
// браузере, и «страница за авторизацией» — повод для отказа. До этих страниц у
// Splitty не было ни одной, а обязательный текст про распознавание голоса и
// чеков человеку негде было прочитать.

func TestPrivacyPageIsPublic(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))

	rec := doRequest(t, s, http.MethodGet, "/privacy", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 без токена — ревьюер магазина открыть её не сможет", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q", ct)
	}
}

// Разделы, без которых страница бесполезна: именно за ними идут и магазины, и
// человек, который хочет понять, куда уходит его голос.
func TestPrivacyPageCoversRequiredTopics(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))
	body := doRequest(t, s, http.MethodGet, "/privacy", "", "").Body.String()

	required := map[string]string{
		"сторонн":            "не сказано, что материал уходит в стороннюю модель",
		"Голос и фото":       "нет раздела про голос и фото чека",
		"Сколько мы храним":  "не сказано, сколько данные хранятся",
		"account-deletion":   "нет ссылки на удаление аккаунта",
		"zagirnur@gmail.com": "не с кем связаться по вопросам данных",
		// Приложение собирает обезличенные события. Расхождение между тем, что
		// оно делает, и тем, что заявлено, ловится на ревью магазина — и это не
		// формальность, а причина отказа.
		"обезличенные события": "не сказано, что приложение собирает события",
		"90 дней": "не сказано, сколько хранятся события",
	}
	for needle, complaint := range required {
		if !strings.Contains(body, needle) {
			t.Errorf("%s (нет %q)", complaint, needle)
		}
	}
}

func TestAccountDeletionPageIsPublic(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))

	rec := doRequest(t, s, http.MethodGet, "/account-deletion", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 без токена", rec.Code)
	}
	body := rec.Body.String()
	for needle, complaint := range map[string]string{
		"Из приложения":  "не описан путь удаления в приложении",
		"Без приложения": "не описан путь удаления без приложения — его требуют магазины",
		"Что остаётся":   "не сказано, что расходы в общих группах остаются",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("%s (нет %q)", complaint, needle)
		}
	}
}

// Страницы не должны требовать настроенного публичного домена: в отличие от
// /join они полезны сами по себе.
func TestLegalPagesWorkWithoutPublicBaseUrl(t *testing.T) {
	s := newTestServer(Config{PublicBaseUrl: ""}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))

	for _, path := range []string{"/privacy", "/account-deletion"} {
		if rec := doRequest(t, s, http.MethodGet, path, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, rec.Code)
		}
	}
}

// Условия подписки — вторая обязательная ссылка с экрана оплаты
// (Guideline 3.1.2). Ссылка, ведущая в никуда, — типовая причина отказа при
// первой подписке, поэтому публичность и наполнение проверяются отдельно.

func TestTermsPageIsPublic(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))

	rec := doRequest(t, s, http.MethodGet, "/terms", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 без токена — ревьюер магазина открыть её не сможет", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("пустое тело страницы условий")
	}
}

// Пункты, которых Apple ждёт от условий подписки. Без любого из них экран
// оплаты не проходит ревью.
func TestTermsPageCoversSubscriptionRequirements(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1), newFakeRoomRepo(newTestRoom()))
	body := doRequest(t, s, http.MethodGet, "/terms", "", "").Body.String()

	required := map[string]string{
		"продлевается автоматически": "не сказано про автопродление",
		"Как отменить":               "не сказано, как отменить подписку",
		"Возврат":                    "нет раздела про возвраты",
		"на месяц или на год":        "не назван срок подписки",
		"/privacy":                   "нет ссылки на политику конфиденциальности",
		"руками":                     "не сказано, что расход можно внести без подписки",
	}
	for needle, complaint := range required {
		if !strings.Contains(body, needle) {
			t.Errorf("%s (нет %q)", complaint, needle)
		}
	}
}
