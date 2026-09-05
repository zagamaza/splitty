package push

import (
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

// TestTokensForDoesNotDoubleDeliverOnMixedClients — главный случай выката:
// один телефон на старой сборке (токена без языка), планшет на новой. Запись
// «без языка» не смеет попасть на планшет: свою он уже получит от записи «en».
func TestTokensForDoesNotDoubleDeliverOnMixedClients(t *testing.T) {
	tokens := []api.PushToken{
		{Token: "phone"},                // старый клиент: языка не шлёт
		{Token: "tablet", Locale: "en"}, // обновился
	}

	legacy := tokensFor(tokens, "")
	if len(legacy) != 1 || legacy[0].Token != "phone" {
		t.Fatalf("запись без языка ушла на %v, ожидался только phone", legacy)
	}
	english := tokensFor(tokens, "en")
	if len(english) != 1 || english[0].Token != "tablet" {
		t.Fatalf("запись en ушла на %v, ожидался только tablet", english)
	}
}

// Записи, лежавшие в очереди до появления языков, доставляются как раньше: у
// устройств такого пользователя языка ещё нет.
func TestTokensForOldQueueRecordReachesEveryone(t *testing.T) {
	tokens := []api.PushToken{{Token: "a"}, {Token: "b"}, {Token: "c"}}
	if got := tokensFor(tokens, ""); len(got) != 3 {
		t.Fatalf("старая запись ушла на %d токенов из 3", len(got))
	}
}

// Язык, которого нет ни у одного устройства: слать некому, и это не ошибка —
// человек успел сменить язык, пока пуш ждал очереди.
func TestTokensForVanishedLocale(t *testing.T) {
	tokens := []api.PushToken{{Token: "a", Locale: "ru"}}
	if got := tokensFor(tokens, "ja"); len(got) != 0 {
		t.Fatalf("нашлись токены для ja: %v", got)
	}
}
