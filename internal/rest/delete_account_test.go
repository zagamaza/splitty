package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// Тесты удаления аккаунта (DELETE /api/v1/me).
//
// Проверяется в первую очередь ПОРЯДОК шагов, а не сам факт удаления: аккаунт
// обязан становиться недоступным раньше, чем что-либо чистится, — иначе сбой
// посередине оставит живой аккаунт с затёртым во всех комнатах именем.

const (
	deletedUserID  = 1 // удаляемый
	survivorUserID = 2 // сосед по комнате
	deletedTgID    = 900900
)

// deleteTestSetup — сервер удаления со всеми подключёнными побочными
// коллекциями и ссылками на фейки, чтобы тест мог заглянуть внутрь
type deleteTestSetup struct {
	s           *Server
	users       *fakeUserRepo
	rooms       *fakeRoomRepo
	codes       *fakeLoginCodeRepo
	chatStates  *fakeChatStates
	bugReports  *fakeChatStates
	pushOutbox    *fakeChatStates
	debtReminders *fakeChatStates
	invites     *fakeInviteStore
	appleTokens *fakeAppleTokens
	room        *api.Room
}

// deleteTestUser — удаляемый: telegram, google и apple разом, плюс refresh
// token Apple. Так один сценарий покрывает и освобождение всех личностей, и
// отзыв токенов
func deleteTestUser() api.User {
	tgID := deletedTgID
	return api.User{
		ID: deletedUserID, Username: "zagir", DisplayName: "Загир",
		TelegramID: &tgID, GoogleSub: "g-del", AppleSub: "a-del",
		Email: "zagir@example.com", AppleRefreshToken: "apple-refresh-1",
		BankDetails: "4276 0000", Aliases: []string{"Заги"},
		PushTokens: []api.PushToken{{Token: "fcm-1"}},
	}
}

// deleteTestRoom — комната с ОБЕИМИ формами операции (легаси recipients и
// современная recipients_with_sum) и с реальными долгами между участниками.
//
// Пользователи кладутся копиями, а не указателями на общие testUserN:
// анонимизация мутирует снимки, и общий указатель протёк бы в соседние тесты
func deleteTestRoom() *api.Room {
	deleted := api.User{ID: deletedUserID, Username: "zagir", DisplayName: "Загир"}
	survivor := api.User{ID: survivorUserID, Username: "almaz", DisplayName: "Алмаз"}

	// легаси: заплатил удаляемый, делили пополам → сосед должен ему 50
	legacy := api.Operation{
		ID: primitive.NewObjectID(), Description: "Ужин", Sum: 100,
		Donor:      &deleted,
		Recipients: &[]api.User{deleted, survivor},
		CreateAt:   time.Now(),
	}
	// современная: заплатил сосед, доли явные → удаляемый должен ему 30
	modern := api.Operation{
		ID: primitive.NewObjectID(), Description: "Такси", Sum: 60,
		Donor:      &survivor,
		Recipients: nil,
		RecipientsWithSum: []api.RecipientWithSum{
			{User: deleted, Sum: 30},
			{User: survivor, Sum: 30},
		},
		CreateAt: time.Now(),
	}
	return &api.Room{
		ID: primitive.NewObjectID(), Name: "Тестовая комната",
		Members:    &[]api.User{deleted, survivor},
		Operations: &[]api.Operation{legacy, modern},
		CreateAt:   time.Now(),
	}
}

func newDeleteSetup(t *testing.T, cfg Config) *deleteTestSetup {
	t.Helper()

	users := newFakeUserRepo(deleteTestUser(), api.User{ID: survivorUserID, Username: "almaz", DisplayName: "Алмаз"})
	room := deleteTestRoom()
	rooms := newFakeRoomRepo(room)
	codes := newFakeLoginCodeRepo()

	apple := &fakeAppleTokens{}
	cfg.AppleTokens = apple

	s := newTestServerWithLoginCodes(cfg, users, rooms, codes)
	set := &deleteTestSetup{
		s: s, users: users, rooms: rooms, codes: codes,
		chatStates: &fakeChatStates{}, bugReports: &fakeChatStates{}, pushOutbox: &fakeChatStates{},
		debtReminders: &fakeChatStates{},
		invites:     newFakeInviteStore(),
		appleTokens: apple, room: room,
	}
	s.SetChatStates(set.chatStates)
	s.SetBugReports(set.bugReports)
	s.SetPushOutbox(set.pushOutbox)
	s.SetInvites(set.invites)
	s.SetDebtReminders(set.debtReminders)
	return set
}

func (d *deleteTestSetup) deleteMe(t *testing.T, userId int) *httptest.ResponseRecorder {
	t.Helper()
	return doRequest(t, d.s, http.MethodDelete, "/api/v1/me", mustToken(t, d.s, userId), "")
}

// debtsOf — долги комнаты глазами живого участника
func (d *deleteTestSetup) debtsOf(t *testing.T, userId int) []debtDto {
	t.Helper()
	rec := doRequest(t, d.s, http.MethodGet,
		"/api/v1/rooms/"+d.room.ID.Hex()+"/debts", mustToken(t, d.s, userId), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET debts: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var debts []debtDto
	if err := json.Unmarshal(rec.Body.Bytes(), &debts); err != nil {
		t.Fatalf("cannot parse debts %q: %v", rec.Body.String(), err)
	}
	return debts
}

// ГЛАВНЫЙ инвариант плана: удаление аккаунта меняет ОТОБРАЖАЕМЫЕ ИМЕНА и больше
// ничего. Долги, посчитанные до и после, обязаны совпасть до копейки — иначе
// человек, удаливший аккаунт, «простил» или «получил» чужие деньги
func TestDeleteMeKeepsDebtsIdentical(t *testing.T) {
	d := newDeleteSetup(t, Config{})

	before := d.debtsOf(t, survivorUserID)
	if len(before) == 0 {
		t.Fatalf("фикстура не даёт долгов — тест инварианта ничего не проверяет")
	}

	if rec := d.deleteMe(t, deletedUserID); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me: status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}

	after := d.debtsOf(t, survivorUserID)
	if len(after) != len(before) {
		t.Fatalf("число долгов изменилось: было %d, стало %d", len(before), len(after))
	}
	for i := range before {
		b, a := before[i], after[i]
		if b.Sum != a.Sum || b.Debtor.ID != a.Debtor.ID || b.Lender.ID != a.Lender.ID {
			t.Errorf("долг %d изменился: было %+v, стало %+v", i, b, a)
		}
	}

	// имя — единственное, что поменялось
	var sawPlaceholder bool
	for _, dto := range after {
		for _, u := range []userDto{dto.Debtor, dto.Lender} {
			if u.ID != deletedUserID {
				continue
			}
			if u.DisplayName != repository.DeletedUserPlaceholder {
				t.Errorf("имя удалённого в долгах = %q, want %q", u.DisplayName, repository.DeletedUserPlaceholder)
			}
			sawPlaceholder = true
		}
	}
	if !sawPlaceholder {
		t.Error("удалённый не встретился в долгах — проверка имени не сработала")
	}

	// сосед не задет
	for _, u := range roomMembers(d.room) {
		if u.ID == survivorUserID && (u.DisplayName != "Алмаз" || u.Username != "almaz") {
			t.Errorf("задет сосед по комнате: %+v", u)
		}
	}
}

// Токен удалённого обязан перестать работать НЕМЕДЛЕННО, а не через accountTTL:
// сам запрос DELETE /me прогревает кеш middleware вердиктом «жив»
func TestDeleteMeInvalidatesTokenImmediately(t *testing.T) {
	d := newDeleteSetup(t, Config{})
	token := mustToken(t, d.s, deletedUserID)

	// прогреваем кеш вердиктом «жив» — именно этот сценарий ломался без forget
	if rec := doRequest(t, d.s, http.MethodGet, "/api/v1/me", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /me до удаления: status = %d, want 200", rec.Code)
	}

	if rec := d.deleteMe(t, deletedUserID); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// без ожидания TTL: те же миллисекунды, тот же токен
	assertErrorCode(t, doRequest(t, d.s, http.MethodGet, "/api/v1/me", token, ""),
		http.StatusUnauthorized, "unauthorized")

	// PATCH /me ходит в SetUserLang через upsert — он НЕ должен воскресить
	// пользователя: middleware обязан остановить запрос раньше хендлера
	assertErrorCode(t, doRequest(t, d.s, http.MethodPatch, "/api/v1/me", token, `{"lang": "en"}`),
		http.StatusUnauthorized, "unauthorized")

	user := d.users.users[deletedUserID]
	if !user.IsDeleted() {
		t.Error("tombstone снят — пользователь воскрешён запросом со старым токеном")
	}
	if user.SelectedLang == "en" {
		t.Error("SetUserLang отработал на удалённом аккаунте")
	}

	// эндпоинты, которые НЕ читают канонический документ, тоже закрыты:
	// currentUser вызывается лишь в 7 хендлерах из ~25, поэтому проверка и
	// живёт в middleware
	assertErrorCode(t, doRequest(t, d.s, http.MethodGet, "/api/v1/rooms/"+d.room.ID.Hex()+"/debts", token, ""),
		http.StatusUnauthorized, "unauthorized")
}

// Tombstone: документ остался (upsert-методы иначе воскресили бы пользователя
// пустышкой), PII вычищена, личности освобождены
func TestDeleteMeTombstoneFreesIdentities(t *testing.T) {
	d := newDeleteSetup(t, Config{})

	if rec := d.deleteMe(t, deletedUserID); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	user, ok := d.users.users[deletedUserID]
	if !ok {
		t.Fatal("документ пользователя удалён — upsert-методы воскресят его пустышкой")
	}
	if !user.IsDeleted() {
		t.Error("deleted_at не выставлен")
	}
	if user.DisplayName != repository.DeletedUserPlaceholder {
		t.Errorf("display_name = %q, want %q", user.DisplayName, repository.DeletedUserPlaceholder)
	}
	if user.Username != "" || user.Email != "" || user.BankDetails != "" ||
		user.AppleRefreshToken != "" || user.Aliases != nil || user.PushTokens != nil {
		t.Errorf("PII осталась в tombstone: %+v", user)
	}

	// личности больше не находятся...
	for name, find := range map[string]func() (*api.User, error){
		"google":   func() (*api.User, error) { return d.users.FindByGoogleSub(context.Background(), "g-del") },
		"apple":    func() (*api.User, error) { return d.users.FindByAppleSub(context.Background(), "a-del") },
		"telegram": func() (*api.User, error) { return d.users.FindByTelegramID(context.Background(), deletedTgID) },
	} {
		if _, err := find(); !errors.Is(err, mongo.ErrNoDocuments) {
			t.Errorf("%s: удалённый всё ещё находится по личности (err=%v)", name, err)
		}
	}
	// ...и той же личностью можно зарегистрироваться заново
	if err := d.users.CreateIdentityUser(context.Background(), api.User{ID: 777, GoogleSub: "g-del"}); err != nil {
		t.Errorf("повторная регистрация с тем же google_sub не удалась: %v", err)
	}
}

// Демо-аккаунт ревьюеров App Store неприкосновенен: tombstone на нём убил бы
// REVIEW_LOGIN_CODE (код продолжал бы выпускать токены, middleware — их
// отвергать), и следующее ревью провалилось бы на той же кнопке
func TestDeleteMeReviewAccountForbidden(t *testing.T) {
	d := newDeleteSetup(t, Config{ReviewUserId: deletedUserID})

	assertErrorCode(t, d.deleteMe(t, deletedUserID), http.StatusForbidden, "forbidden")

	if d.users.users[deletedUserID].IsDeleted() {
		t.Fatal("демо-аккаунт всё-таки помечен удалённым")
	}
	if len(d.appleTokens.revoked) != 0 {
		t.Errorf("у демо-аккаунта отозваны токены Apple: %v", d.appleTokens.revoked)
	}
	// обычному пользователю запрет не мешает
	d.users.users[survivorUserID].GoogleSub = "g-survivor"
	if rec := d.deleteMe(t, survivorUserID); rec.Code != http.StatusNoContent {
		t.Fatalf("удаление обычного аккаунта: status = %d, body: %s", rec.Code, rec.Body.String())
	}
}

// Повторный DELETE /me не падает: маршрут висит на authDeleted именно затем,
// чтобы упавший после tombstone запрос было кому довести до конца
func TestDeleteMeIsRepeatable(t *testing.T) {
	d := newDeleteSetup(t, Config{})
	token := mustToken(t, d.s, deletedUserID)

	for i := 0; i < 2; i++ {
		rec := doRequest(t, d.s, http.MethodDelete, "/api/v1/me", token, "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("DELETE /me (попытка %d): status = %d, body: %s", i+1, rec.Code, rec.Body.String())
		}
	}
	// revoke только на первом проходе: refresh token вычистил шаг 1
	if len(d.appleTokens.revoked) != 1 {
		t.Errorf("revoke вызван %d раз, want 1 (на повторе отзывать уже нечего)", len(d.appleTokens.revoked))
	}
}

// ⚠️ Сбой ДО tombstone обязан отличаться кодом от сбоя ПОСЛЕ. Снаружи это два
// одинаковых 500, но последствия у клиента противоположные: здесь аккаунт ЖИВ и
// не тронут, и клиент обязан сохранить сессию вместе с очередью неотправленных
// офлайн-расходов (iOS стирал их на любом 500 — транзиентный сбой mongo уносил
// очередь при целом аккаунте). Одинаковые коды делали это неразличимым
func TestDeleteMePreTombstoneFailureKeepsAccountAlive(t *testing.T) {
	d := newDeleteSetup(t, Config{})
	d.users.softDeleteErr = errors.New("mongo недоступен")

	assertErrorCode(t, d.deleteMe(t, deletedUserID),
		http.StatusInternalServerError, errCodeInternal)

	if d.users.users[deletedUserID].IsDeleted() {
		t.Error("аккаунт помечен удалённым, хотя tombstone упал")
	}
	if len(d.rooms.anonymized) != 0 {
		t.Errorf("анонимизация отработала до tombstone: %v", d.rooms.anonymized)
	}

	// Аккаунт остался пригодным: тот же токен продолжает работать
	token := mustToken(t, d.s, deletedUserID)
	if rec := doRequest(t, d.s, http.MethodGet, "/api/v1/me", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /me после несостоявшегося удаления: status = %d, want 200", rec.Code)
	}
}

// ⚠️ Тест порядка шагов. Сбой анонимизации обязан застать аккаунт УЖЕ
// недоступным. Обратный порядок (анонимизация → tombstone) оставил бы живой
// аккаунт с затёртым во всех комнатах именем — необратимо, снимки из
// канонического документа не перестраиваются
func TestDeleteMePartialFailureLeavesAccountUnusable(t *testing.T) {
	d := newDeleteSetup(t, Config{})
	token := mustToken(t, d.s, deletedUserID)
	d.rooms.anonymizeErr = errors.New("mongo недоступен")

	// Код обязан отличаться от «internal»: по нему клиент понимает, что
	// tombstone УЖЕ стоит, и обязан сохранить токен — повторить удаление больше
	// нечем (войти заново в удалённый аккаунт нельзя, личности вычищены)
	rec := doRequest(t, d.s, http.MethodDelete, "/api/v1/me", token, "")
	assertErrorCode(t, rec, http.StatusInternalServerError, errCodePurgeIncomplete)

	// аккаунт уже недоступен, а НЕ «жив, но с затёртым именем»
	if !d.users.users[deletedUserID].IsDeleted() {
		t.Fatal("tombstone не поставлен до анонимизации — сбой оставил аккаунт живым")
	}
	assertErrorCode(t, doRequest(t, d.s, http.MethodGet, "/api/v1/me", token, ""),
		http.StatusUnauthorized, "unauthorized")
	if len(d.rooms.anonymized) != 0 {
		t.Fatalf("анонимизация отработала, хотя должна была упасть: %v", d.rooms.anonymized)
	}

	// повторный вызов доводит дело до конца
	if rec := doRequest(t, d.s, http.MethodDelete, "/api/v1/me", token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("повторный DELETE /me: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if len(d.rooms.anonymized) != 1 {
		t.Errorf("анонимизация после повтора: %v, want один вызов по %d", d.rooms.anonymized, deletedUserID)
	}
	for _, u := range roomMembers(d.room) {
		if u.ID == deletedUserID && u.DisplayName != repository.DeletedUserPlaceholder {
			t.Errorf("снимок не анонимизирован после повтора: %+v", u)
		}
	}
}

// Побочные коллекции хранят реальный PII, а не технический мусор: текст расхода
// (chat_state), свободный текст жалобы (bug_report), отрендеренные title/body с
// именем автора (push_outbox) и живой код входа (login_code)
func TestDeleteMePurgesSideCollections(t *testing.T) {
	d := newDeleteSetup(t, Config{})
	if err := d.codes.SaveLoginCode(context.Background(), &api.LoginCode{
		Code: "AAAAAA", UserId: deletedUserID, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveLoginCode: %v", err)
	}

	if rec := d.deleteMe(t, deletedUserID); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// chat_state чистится и по каноническому _id, и по сырому telegram id:
	// состояния исторически сохранялись по обоим
	wantChatIDs := map[int]bool{deletedUserID: false, deletedTgID: false}
	for _, id := range d.chatStates.deleted {
		if _, ok := wantChatIDs[id]; !ok {
			t.Errorf("chat_state почищен по постороннему id %d", id)
			continue
		}
		wantChatIDs[id] = true
	}
	for id, seen := range wantChatIDs {
		if !seen {
			t.Errorf("chat_state не почищен по id %d", id)
		}
	}

	for name, cleaner := range map[string]*fakeChatStates{"bug_report": d.bugReports, "push_outbox": d.pushOutbox, "debt_reminder": d.debtReminders} {
		if len(cleaner.deleted) != 1 || cleaner.deleted[0] != deletedUserID {
			t.Errorf("%s: DeleteByUserId вызван с %v, want [%d]", name, cleaner.deleted, deletedUserID)
		}
	}
	if len(d.codes.codes) != 0 {
		t.Errorf("живой код входа пережил удаление аккаунта: %v", d.codes.codes)
	}
}

// ⚠️ Регрессия: chat_state, сохранённый под СЫРЫМ telegram id, не должен
// застревать в базе навсегда, если удаление упало ПОСЛЕ tombstone.
//
// chat_state.external_data хранит свободный текст расхода — настоящий PII, а
// часть записей лежит под telegram id (chatStateIDs). SoftDeleteUser вычищает
// telegram_id первым же действием, поэтому повторный DELETE /me видит в
// документе только канонический _id: пока чистка стояла ПОСЛЕ tombstone,
// первый же сбой на анонимизации оставлял telegram-ключ недостижимым, и повтор
// уже ничем не мог помочь. Отсюда шаг (1) — chat_state чистится ДО tombstone
func TestDeleteMePurgesTelegramChatStatesBeforeTombstone(t *testing.T) {
	d := newDeleteSetup(t, Config{})
	// сбой на шаге (4): аккаунт уже удалён, PII в комнатах ещё нет
	d.rooms.anonymizeErr = errors.New("mongo недоступен")

	assertErrorCode(t, d.deleteMe(t, deletedUserID),
		http.StatusInternalServerError, errCodePurgeIncomplete)

	for _, id := range []int{deletedUserID, deletedTgID} {
		if !containsInt(d.chatStates.deleted, id) {
			t.Fatalf("после сбоя chat_state не почищен по id %d (почищено %v): "+
				"telegram-ключ потерян вместе с tombstone", id, d.chatStates.deleted)
		}
	}

	// повтор доводит удаление до конца; telegram id из документа уже пропал,
	// поэтому второй проход чистит только канонический _id — и этого достаточно
	before := len(d.chatStates.deleted)
	if rec := d.deleteMe(t, deletedUserID); rec.Code != http.StatusNoContent {
		t.Fatalf("повторный DELETE /me: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	for _, id := range d.chatStates.deleted[before:] {
		if id != deletedUserID {
			t.Errorf("повтор почистил chat_state по неожиданному id %d", id)
		}
	}
}

func containsInt(ids []int, want int) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// Сбой чистки побочных коллекций — 500, а не тихое «удалили»: клиент обязан
// повторить, а не думать, что PII убрана
func TestDeleteMePurgeFailureReportsError(t *testing.T) {
	d := newDeleteSetup(t, Config{})
	d.bugReports.err = errors.New("mongo недоступен")

	assertErrorCode(t, d.deleteMe(t, deletedUserID),
		http.StatusInternalServerError, errCodePurgeIncomplete)
	if !d.users.users[deletedUserID].IsDeleted() {
		t.Error("аккаунт остался живым при сбое на последнем шаге")
	}

	d.bugReports.err = nil
	if rec := d.deleteMe(t, deletedUserID); rec.Code != http.StatusNoContent {
		t.Fatalf("повторный DELETE /me: status = %d, body: %s", rec.Code, rec.Body.String())
	}
}

// Отзыв токенов Apple (Guideline 5.1.1(v)): без него ревью отказывает ровно на
// той кнопке, ради которой сделана вся задача
func TestDeleteMeRevokesAppleTokens(t *testing.T) {
	tests := []struct {
		name        string
		refresh     string
		revokeErr   error
		noAppleCfg  bool
		wantRevoked []string
	}{
		{
			name:        "есть refresh token — отзываем",
			refresh:     "apple-refresh-1",
			wantRevoked: []string{"apple-refresh-1"},
		},
		{
			// Apple лежит или токен уже отозван — не повод отказать человеку
			// в удалении аккаунта
			name:        "ошибка отзыва не мешает удалению",
			refresh:     "apple-refresh-1",
			revokeErr:   errors.New("apple недоступен"),
			wantRevoked: []string{"apple-refresh-1"},
		},
		{
			name:        "без Apple отзывать нечего",
			refresh:     "",
			wantRevoked: nil,
		},
		{
			// локальная разработка без ключа .p8
			name:        "AppleTokens не сконфигурирован",
			refresh:     "apple-refresh-1",
			noAppleCfg:  true,
			wantRevoked: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDeleteSetup(t, Config{})
			d.users.users[deletedUserID].AppleRefreshToken = tc.refresh
			d.appleTokens.revokeErr = tc.revokeErr
			if tc.noAppleCfg {
				d.s.cfg.AppleTokens = nil
			}

			if rec := d.deleteMe(t, deletedUserID); rec.Code != http.StatusNoContent {
				t.Fatalf("DELETE /me: status = %d, want 204, body: %s", rec.Code, rec.Body.String())
			}
			if !d.users.users[deletedUserID].IsDeleted() {
				t.Error("аккаунт не помечен удалённым")
			}
			if fmt.Sprint(d.appleTokens.revoked) != fmt.Sprint(tc.wantRevoked) {
				t.Errorf("revoked = %v, want %v", d.appleTokens.revoked, tc.wantRevoked)
			}
		})
	}
}

// Кеш вердиктов middleware: без него проверка tombstone стоила бы запроса в
// mongo на каждый авторизованный запрос
func TestAccountCache(t *testing.T) {
	now := time.Now()
	c := newAccountCache()
	c.now = func() time.Time { return now }

	if _, hit := c.get(42); hit {
		t.Error("пустой кеш ответил попаданием")
	}

	c.put(42, true)
	if alive, hit := c.get(42); !hit || !alive {
		t.Errorf("get после put = (%v, %v), want (true, true)", alive, hit)
	}

	// протухание
	now = now.Add(accountTTL + time.Second)
	if _, hit := c.get(42); hit {
		t.Error("запись пережила TTL")
	}

	// forget — то, чем handleDeleteMe гасит собственный прогрев кеша
	c.put(43, true)
	c.forget(43)
	if _, hit := c.get(43); hit {
		t.Error("forget не удалил запись")
	}

	// переполнение сбрасывает кеш целиком, а не растёт безгранично
	c.max = 3
	for i := 0; i < 10; i++ {
		c.put(i, true)
	}
	if len(c.entries) > c.max {
		t.Errorf("в кеше %d записей при потолке %d", len(c.entries), c.max)
	}
}

// TestDeleteMePurgesRoomInvites — приглашения удалённого обязаны исчезнуть с
// ОБЕИХ сторон: и там, где он приглашённый, и там, где он приглашающий (в
// inviter_id лежит его же id, и связь «кто кого звал» это тоже персональные
// данные). Тест обязан падать, если коллекцию забудут подключить к чистке.
func TestDeleteMePurgesRoomInvites(t *testing.T) {
	set := newDeleteSetup(t, Config{})
	ctx := context.Background()

	asInvitee := primitive.NewObjectID()
	asInviter := primitive.NewObjectID()
	foreign := primitive.NewObjectID()

	if err := set.invites.Upsert(ctx, asInvitee, deletedUserID, survivorUserID, api.InviteAdded, time.Now()); err != nil {
		t.Fatalf("подготовка приглашения к удаляемому: %v", err)
	}
	if err := set.invites.Upsert(ctx, asInviter, survivorUserID, deletedUserID, api.InvitePending, time.Now()); err != nil {
		t.Fatalf("подготовка приглашения от удаляемого: %v", err)
	}
	if err := set.invites.Upsert(ctx, foreign, survivorUserID, 999, api.InviteAdded, time.Now()); err != nil {
		t.Fatalf("подготовка чужого приглашения: %v", err)
	}

	if rec := set.deleteMe(t, deletedUserID); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me: ожидался 204, получен %d (%s)", rec.Code, rec.Body.String())
	}

	if _, err := set.invites.Find(ctx, asInvitee, deletedUserID); err != mongo.ErrNoDocuments {
		t.Fatalf("приглашение К удалённому осталось: %v", err)
	}
	if _, err := set.invites.Find(ctx, asInviter, survivorUserID); err != mongo.ErrNoDocuments {
		t.Fatalf("приглашение ОТ удалённого осталось: %v", err)
	}
	if _, err := set.invites.Find(ctx, foreign, survivorUserID); err != nil {
		t.Fatalf("чужое приглашение пострадало: %v", err)
	}
}
