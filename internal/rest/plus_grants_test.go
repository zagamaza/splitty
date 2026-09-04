package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"github.com/almaznur91/splitty/internal/service"
)

// fakeGrantStore — гранты в памяти с тем же контрактом, что у mongo-репозитория:
// LiveByUser отдаёт только живой, DeleteByUserId уносит все строки человека.
type fakeGrantStore struct {
	byUser  map[int]*api.PlusGrant
	deleted []int
	err     error
}

func newFakeGrantStore() *fakeGrantStore {
	return &fakeGrantStore{byUser: map[int]*api.PlusGrant{}}
}

func (f *fakeGrantStore) LiveByUser(_ context.Context, userId int, now time.Time) (*api.PlusGrant, error) {
	if f.err != nil {
		return nil, f.err
	}
	g := f.byUser[userId]
	if !g.Live(now) {
		return nil, nil
	}
	return g, nil
}

func (f *fakeGrantStore) DeleteByUserId(_ context.Context, userId int) error {
	f.deleted = append(f.deleted, userId)
	delete(f.byUser, userId)
	return nil
}

func (f *fakeGrantStore) Grant(_ context.Context, userId int, expiresAt time.Time, reason string, now time.Time) error {
	if f.err != nil {
		return f.err
	}
	existing := f.byUser[userId]
	// Как в mongo: живой грант продлевается, причина затирается только новой
	// непустой; живого нет — заводится строка.
	if existing.Live(now) {
		existing.ExpiresAt = expiresAt
		if reason != "" {
			existing.Reason = reason
		}
		existing.UpdatedAt = now
		return nil
	}
	f.byUser[userId] = &api.PlusGrant{
		UserId:    userId,
		Source:    api.GrantSourcePanel,
		Reason:    reason,
		ExpiresAt: expiresAt,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return nil
}

func (f *fakeGrantStore) Revoke(_ context.Context, userId int, reason string, at time.Time) error {
	if f.err != nil {
		return f.err
	}
	if g := f.byUser[userId]; g != nil {
		g.RevokedAt = &at
		g.RevokedReason = reason
	}
	return nil
}

func (f *fakeGrantStore) ListLive(_ context.Context, now time.Time) ([]api.PlusGrant, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]api.PlusGrant, 0, len(f.byUser))
	for _, g := range f.byUser {
		if g.Live(now) {
			out = append(out, *g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExpiresAt.After(out[j].ExpiresAt) })
	return out, nil
}

// give — прямая подстановка живого гранта в тестах, мимо контракта Grant.
func (f *fakeGrantStore) give(userId int, expires time.Time) {
	f.byUser[userId] = &api.PlusGrant{
		UserId:    userId,
		Source:    api.GrantSourcePanel,
		ExpiresAt: expires,
		CreatedAt: time.Now().UTC(),
	}
}

// grantServer — сервер с подписками и грантами, как в проде.
func grantServer(t *testing.T, subs *fakeSubStore, grants *fakeGrantStore) *Server {
	t.Helper()
	s := subServer(t, subs, &fakeVerifier{receipt: goodReceipt()}, &fakeAck{})
	ent := service.NewEntitlements(subs, service.EntitlementsConfig{
		FreeQuota: 5, PlusQuota: service.UnlimitedQuota, LegacyQuota: 50,
		DeliverySlack: 2 * time.Hour,
	})
	ent.SetGrants(grants)
	s.SetEntitlements(ent)
	s.SetPlusGrants(grants)
	s.SetDeliverySlack(2 * time.Hour)
	return s
}

func getSubscriptionState(t *testing.T, s *Server, userId int) subscriptionDto {
	t.Helper()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/me/subscription", mustToken(t, s, userId), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	var dto subscriptionDto
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("разбор ответа: %v (%s)", err, rec.Body.String())
	}
	return dto
}

// Истёкшая покупка + живой грант: показывается дата ГРАНТА, а реквизиты покупки
// не показываются вовсе.
//
// Раньше сюда приезжала дата из прошлого и живая ссылка «Управлять подпиской» в
// стор, где ничего нет: ActiveByUser намеренно отдаёт истёкшие, а экран брал
// максимальную дату, ни разу не спросив Active. Пока tier был free, это было
// безвредно; с грантом tier стал plus — и ровно та аудитория, которой гранты
// раздают, увидела бы прошлогоднюю дату.
func TestSubscriptionStateGrantBeatsExpiredPurchase(t *testing.T) {
	subs := newFakeSubStore()
	subs.byRef[refKey(api.StoreApple, "old")] = &api.Subscription{
		UserId: testUser1.ID, Store: api.StoreApple, ProductId: "monthly",
		StoreRef: "old", ExpiresAt: time.Now().Add(-365 * 24 * time.Hour), AutoRenew: true,
	}
	grants := newFakeGrantStore()
	granted := time.Now().UTC().Add(30 * 24 * time.Hour)
	grants.give(testUser1.ID, granted)

	dto := getSubscriptionState(t, grantServer(t, subs, grants), testUser1.ID)

	if dto.Tier != api.TierPlus {
		t.Fatalf("тариф %q, ожидал plus", dto.Tier)
	}
	if dto.ExpiresAt == nil || !dto.ExpiresAt.Equal(granted) {
		t.Fatalf("срок %v, ожидал срок гранта %v", dto.ExpiresAt, granted)
	}
	if dto.Store != "" || dto.ProductId != "" || dto.ManageUrl != "" || dto.AutoRenew {
		t.Fatalf("реквизиты истёкшей покупки просочились: %+v", dto)
	}
}

// Живая покупка + грант: побеждает бо́льшая дата, и когда это покупка —
// реквизиты остаются на месте.
func TestSubscriptionStateLivePurchaseKeepsItsDetails(t *testing.T) {
	subs := newFakeSubStore()
	purchaseExpires := time.Now().UTC().Add(90 * 24 * time.Hour)
	subs.byRef[refKey(api.StoreApple, "live")] = &api.Subscription{
		UserId: testUser1.ID, Store: api.StoreApple, ProductId: "yearly",
		StoreRef: "live", ExpiresAt: purchaseExpires, AutoRenew: true,
	}
	grants := newFakeGrantStore()
	grants.give(testUser1.ID, time.Now().UTC().Add(7*24*time.Hour))

	dto := getSubscriptionState(t, grantServer(t, subs, grants), testUser1.ID)

	if dto.ExpiresAt == nil || !dto.ExpiresAt.Equal(purchaseExpires) {
		t.Fatalf("срок %v, ожидал срок покупки %v", dto.ExpiresAt, purchaseExpires)
	}
	if dto.Store != api.StoreApple || dto.ProductId != "yearly" || dto.ManageUrl == "" || !dto.AutoRenew {
		t.Fatalf("реквизиты живой покупки потерялись: %+v", dto)
	}
}

// Грант дальше живой покупки: дата гранта, но реквизиты снимаются — ссылка
// «управлять» относилась бы к покупке, которая заканчивается раньше.
func TestSubscriptionStateGrantOutlastingPurchaseDropsDetails(t *testing.T) {
	subs := newFakeSubStore()
	subs.byRef[refKey(api.StoreGoogle, "live")] = &api.Subscription{
		UserId: testUser1.ID, Store: api.StoreGoogle, ProductId: "monthly",
		StoreRef: "live", ExpiresAt: time.Now().UTC().Add(7 * 24 * time.Hour), AutoRenew: true,
	}
	grants := newFakeGrantStore()
	granted := time.Now().UTC().Add(180 * 24 * time.Hour)
	grants.give(testUser1.ID, granted)

	dto := getSubscriptionState(t, grantServer(t, subs, grants), testUser1.ID)

	if dto.ExpiresAt == nil || !dto.ExpiresAt.Equal(granted) {
		t.Fatalf("срок %v, ожидал срок гранта %v", dto.ExpiresAt, granted)
	}
	if dto.Store != "" || dto.ManageUrl != "" {
		t.Fatalf("реквизиты покупки остались при победившем гранте: %+v", dto)
	}
}

// Покупка в окне продления остаётся живой: у платящего не должна пропадать
// ссылка «Управлять подпиской» на те два часа, что резолв тарифа ему прощает.
func TestSubscriptionStateKeepsDetailsInsideDeliverySlack(t *testing.T) {
	subs := newFakeSubStore()
	expired := time.Now().UTC().Add(-time.Hour) // истекла час назад, запас — два
	subs.byRef[refKey(api.StoreApple, "renewing")] = &api.Subscription{
		UserId: testUser1.ID, Store: api.StoreApple, ProductId: "monthly",
		StoreRef: "renewing", ExpiresAt: expired, AutoRenew: true,
	}

	dto := getSubscriptionState(t, grantServer(t, subs, newFakeGrantStore()), testUser1.ID)

	if dto.Store != api.StoreApple || dto.ManageUrl == "" {
		t.Fatalf("в окне продления пропали реквизиты: %+v", dto)
	}
}

// Истёкшая покупка БЕЗ гранта не показывает ни даты, ни ссылки.
//
// Это проверка самой фильтрации по живости, отдельно от грантов: в тесте с
// грантом она незаметна — там победивший грант всё равно затирает реквизиты, и
// пропажа проверки прошла бы мимо. Здесь затирать некому, и «Активна до» с
// прошлогодней датой и ссылкой в стор видно сразу.
func TestSubscriptionStateHidesExpiredPurchase(t *testing.T) {
	subs := newFakeSubStore()
	subs.byRef[refKey(api.StoreApple, "old")] = &api.Subscription{
		UserId: testUser1.ID, Store: api.StoreApple, ProductId: "monthly",
		StoreRef: "old", ExpiresAt: time.Now().UTC().Add(-365 * 24 * time.Hour), AutoRenew: true,
	}

	dto := getSubscriptionState(t, grantServer(t, subs, newFakeGrantStore()), testUser1.ID)

	if dto.ExpiresAt != nil {
		t.Fatalf("показана дата истёкшей подписки: %v", dto.ExpiresAt)
	}
	if dto.Store != "" || dto.ManageUrl != "" || dto.ProductId != "" || dto.AutoRenew {
		t.Fatalf("реквизиты истёкшей подписки показаны: %+v", dto)
	}
}

// Без грантов экран ведёт себя ровно как раньше.
func TestSubscriptionStateWithoutGrantsUnchanged(t *testing.T) {
	subs := newFakeSubStore()
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	subs.byRef[refKey(api.StoreApple, "live")] = &api.Subscription{
		UserId: testUser1.ID, Store: api.StoreApple, ProductId: "monthly",
		StoreRef: "live", ExpiresAt: expires, AutoRenew: true,
	}
	s := subServer(t, subs, &fakeVerifier{receipt: goodReceipt()}, &fakeAck{})
	s.SetDeliverySlack(2 * time.Hour)

	dto := getSubscriptionState(t, s, testUser1.ID)

	if dto.ExpiresAt == nil || !dto.ExpiresAt.Equal(expires) || dto.Store != api.StoreApple {
		t.Fatalf("поведение без грантов изменилось: %+v", dto)
	}
}

// --- админские маршруты выдачи ---

// adminGrantServer — админский сервер с грантами, подписками и резолвом тарифа.
func adminGrantServer(t *testing.T, deletedUser bool) (*Server, *fakeGrantStore, *fakeSubStore) {
	t.Helper()
	user := testUser1
	if deletedUser {
		at := time.Now().UTC()
		user.DeletedAt = &at
	}
	users := newFakeUserRepo(user, testUser2)
	s := newTestServer(Config{AdminToken: testAdminToken}, users, newFakeRoomRepo(newTestRoom()))
	subs := newFakeSubStore()
	grants := newFakeGrantStore()
	ent := service.NewEntitlements(subs, service.EntitlementsConfig{
		FreeQuota: 5, PlusQuota: service.UnlimitedQuota, LegacyQuota: 50,
		DeliverySlack: 2 * time.Hour,
	})
	ent.SetGrants(grants)
	s.SetEntitlements(ent)
	s.SetSubscriptions(subs, nil, nil, nil)
	s.SetPlusGrants(grants)
	s.SetDeliverySlack(2 * time.Hour)
	return s, grants, subs
}

func grantBody(expires time.Time, reason string) string {
	b, _ := json.Marshal(grantRequest{ExpiresAt: expires, Reason: reason})
	return string(b)
}

// Выдача заводит грант, отдаёт состояние и делает Plus видимым СРАЗУ: без
// сброса кеша подарок выглядел бы поломкой.
func TestAdminGrantPlus(t *testing.T) {
	s, grants, _ := adminGrantServer(t, false)
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)

	rec := doAdminMethod(t, s, http.MethodPost,
		"/admin/users/"+strconv.Itoa(testUser1.ID)+"/plus", testAdminToken,
		grantBody(expires, "друг"))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}

	var state adminUserPlusDto
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("разбор: %v (%s)", err, rec.Body.String())
	}
	if state.Tier != api.TierPlus || state.Source != plusSourceGrant || state.Reason != "друг" {
		t.Fatalf("состояние после выдачи: %+v", state)
	}
	if g := grants.byUser[testUser1.ID]; g == nil || !g.ExpiresAt.Equal(expires) {
		t.Fatalf("грант не записан: %+v", g)
	}
}

// Повторная выдача продлевает ту же строку.
func TestAdminGrantPlusExtends(t *testing.T) {
	s, grants, _ := adminGrantServer(t, false)
	path := "/admin/users/" + strconv.Itoa(testUser1.ID) + "/plus"

	doAdminMethod(t, s, http.MethodPost, path, testAdminToken,
		grantBody(time.Now().UTC().Add(24*time.Hour), "первый"))
	longer := time.Now().UTC().Add(90 * 24 * time.Hour)
	rec := doAdminMethod(t, s, http.MethodPost, path, testAdminToken, grantBody(longer, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}

	g := grants.byUser[testUser1.ID]
	if g == nil || !g.ExpiresAt.Equal(longer) {
		t.Fatalf("срок не продлился: %+v", g)
	}
	if g.Reason != "первый" {
		t.Fatalf("продление без причины стёрло прежнюю: %q", g.Reason)
	}
}

// Срок в прошлом и срок дальше двух лет — 400.
func TestAdminGrantPlusRejectsBadHorizon(t *testing.T) {
	s, _, _ := adminGrantServer(t, false)
	path := "/admin/users/" + strconv.Itoa(testUser1.ID) + "/plus"

	for _, c := range []struct {
		name    string
		expires time.Time
	}{
		{"в прошлом", time.Now().UTC().Add(-time.Hour)},
		{"дальше двух лет", time.Now().UTC().Add(3 * 365 * 24 * time.Hour)},
		{"пустой", time.Time{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := doAdminMethod(t, s, http.MethodPost, path, testAdminToken, grantBody(c.expires, ""))
			assertErrorCode(t, rec, http.StatusBadRequest, "validation")
		})
	}
}

// Причина считается в РУНАХ: 200 кириллических символов — это 400 байт, и лимит
// по байтам зарезал бы русский текст вдвое раньше.
func TestAdminGrantPlusReasonCountedInRunes(t *testing.T) {
	s, _, _ := adminGrantServer(t, false)
	path := "/admin/users/" + strconv.Itoa(testUser1.ID) + "/plus"
	expires := time.Now().UTC().Add(24 * time.Hour)

	ok := strings.Repeat("я", maxGrantReasonRunes)
	if rec := doAdminMethod(t, s, http.MethodPost, path, testAdminToken, grantBody(expires, ok)); rec.Code != http.StatusOK {
		t.Fatalf("двести кириллических символов отвергнуты: %d %s", rec.Code, rec.Body.String())
	}

	tooLong := strings.Repeat("я", maxGrantReasonRunes+1)
	rec := doAdminMethod(t, s, http.MethodPost, path, testAdminToken, grantBody(expires, tooLong))
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")
}

// Несуществующий человек — 404.
func TestAdminGrantPlusUnknownUser(t *testing.T) {
	s, _, _ := adminGrantServer(t, false)
	rec := doAdminMethod(t, s, http.MethodPost, "/admin/users/999999/plus", testAdminToken,
		grantBody(time.Now().UTC().Add(24*time.Hour), ""))
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
}

// Удалённый — тоже 404.
//
// Отдельным тестом, потому что промахнуться здесь легко: FindById tombstone не
// фильтрует, а карточка человека удалённого намеренно ВОЗВРАЩАЕТ. Без явной
// проверки грант на удалённый аккаунт прошёл бы с 200 OK.
func TestAdminGrantPlusDeletedUser(t *testing.T) {
	s, grants, _ := adminGrantServer(t, true)

	rec := doAdminMethod(t, s, http.MethodPost,
		"/admin/users/"+strconv.Itoa(testUser1.ID)+"/plus", testAdminToken,
		grantBody(time.Now().UTC().Add(24*time.Hour), ""))
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")

	if len(grants.byUser) != 0 {
		t.Fatalf("грант выдан удалённому: %+v", grants.byUser)
	}
}

// Отзыв снимает Plus и виден сразу.
func TestAdminRevokePlus(t *testing.T) {
	s, grants, _ := adminGrantServer(t, false)
	grants.give(testUser1.ID, time.Now().UTC().Add(30*24*time.Hour))

	rec := doAdminMethod(t, s, http.MethodDelete,
		"/admin/users/"+strconv.Itoa(testUser1.ID)+"/plus", testAdminToken,
		`{"reason":"передумали"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}

	var state adminUserPlusDto
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if state.Tier != api.TierFree {
		t.Fatalf("после отзыва тариф %q", state.Tier)
	}
	if g := grants.byUser[testUser1.ID]; g == nil || g.RevokedAt == nil || g.RevokedReason != "передумали" {
		t.Fatalf("отзыв не записан: %+v", g)
	}
}

// Отзыв без тела — рабочий случай, а не 400.
func TestAdminRevokePlusWithoutBody(t *testing.T) {
	s, grants, _ := adminGrantServer(t, false)
	grants.give(testUser1.ID, time.Now().UTC().Add(30*24*time.Hour))

	rec := doAdminMethod(t, s, http.MethodDelete,
		"/admin/users/"+strconv.Itoa(testUser1.ID)+"/plus", testAdminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
}

// Список выдан с именами: иначе панель покажет голые номера.
func TestAdminPlusListCarriesNames(t *testing.T) {
	s, grants, _ := adminGrantServer(t, false)
	grants.give(testUser1.ID, time.Now().UTC().Add(30*24*time.Hour))

	rec := doAdmin(t, s, "/admin/plus", testAdminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}

	var items []adminPlusDto
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("разбор: %v (%s)", err, rec.Body.String())
	}
	if len(items) != 1 {
		t.Fatalf("строк %d, ожидал одну: %+v", len(items), items)
	}
	if items[0].DisplayName != testUser1.DisplayName {
		t.Fatalf("имя не подставлено: %+v", items[0])
	}
}

// Карточка человека различает покупку, грант и список в окружении.
func TestAdminUserCardPlusSource(t *testing.T) {
	t.Run("покупка", func(t *testing.T) {
		s, _, subs := adminGrantServer(t, false)
		subs.byRef[refKey(api.StoreApple, "live")] = &api.Subscription{
			UserId: testUser1.ID, Store: api.StoreApple, StoreRef: "live",
			ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour),
		}
		assertCardSource(t, s, plusSourcePurchase)
	})

	t.Run("грант", func(t *testing.T) {
		s, grants, _ := adminGrantServer(t, false)
		grants.give(testUser1.ID, time.Now().UTC().Add(30*24*time.Hour))
		assertCardSource(t, s, plusSourceGrant)
	})

	t.Run("список в окружении", func(t *testing.T) {
		s, _, subs := adminGrantServer(t, false)
		ent := service.NewEntitlements(subs, service.EntitlementsConfig{
			FreeQuota: 5, PlusQuota: service.UnlimitedQuota, LegacyQuota: 50,
			CompUserIds: []int{testUser1.ID},
		})
		s.SetEntitlements(ent)
		assertCardSource(t, s, plusSourceComp)
	})

	t.Run("бесплатный", func(t *testing.T) {
		s, _, _ := adminGrantServer(t, false)
		card := adminCard(t, s)
		if card.Plus == nil || card.Plus.Tier != api.TierFree || card.Plus.Source != "" {
			t.Fatalf("бесплатный показан как платный: %+v", card.Plus)
		}
	})
}

func adminCard(t *testing.T, s *Server) adminUserDto {
	t.Helper()
	rec := doAdmin(t, s, "/admin/users/"+strconv.Itoa(testUser1.ID), testAdminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	var card adminUserDto
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
		t.Fatalf("разбор: %v (%s)", err, rec.Body.String())
	}
	return card
}

func assertCardSource(t *testing.T, s *Server, want string) {
	t.Helper()
	card := adminCard(t, s)
	if card.Plus == nil {
		t.Fatal("блока plus нет в карточке")
	}
	if card.Plus.Tier != api.TierPlus || card.Plus.Source != want {
		t.Fatalf("источник %q, ожидал %q: %+v", card.Plus.Source, want, card.Plus)
	}
}

// Без токена не пускает ни выдача, ни отзыв.
func TestAdminPlusRequiresToken(t *testing.T) {
	s, _, _ := adminGrantServer(t, false)
	path := "/admin/users/" + strconv.Itoa(testUser1.ID) + "/plus"

	for _, m := range []string{http.MethodPost, http.MethodDelete} {
		rec := doAdminMethod(t, s, m, path, "", grantBody(time.Now().UTC().Add(24*time.Hour), ""))
		assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	}
}

// GET на пишущий путь не маршрутизируется: «удобная ссылка» открывала бы выдачу
// Plus по клику из мессенджера (кука панели SameSite: Lax).
func TestAdminPlusGrantNotReachableByGet(t *testing.T) {
	s, _, _ := adminGrantServer(t, false)
	rec := doAdmin(t, s, "/admin/users/"+strconv.Itoa(testUser1.ID)+"/plus", testAdminToken)
	assertErrorCode(t, rec, http.StatusNotFound, "not_found")
}

// Источник совпадает с порядком РЕЗОЛВА, а не с «у кого дата дальше».
//
// У человека и живая покупка, и подарок подлиннее. Резолв при живой покупке до
// подарков не доходит вовсе, поэтому панель обязана сказать «платит» — иначе
// она объявит подарком того, кому Plus на самом деле даёт покупка.
func TestAdminPlusSourceFollowsResolverNotLongestDate(t *testing.T) {
	s, grants, subs := adminGrantServer(t, false)
	subs.byRef[refKey(api.StoreApple, "live")] = &api.Subscription{
		UserId: testUser1.ID, Store: api.StoreApple, StoreRef: "live",
		ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour),
	}
	grants.give(testUser1.ID, time.Now().UTC().Add(365*24*time.Hour))

	card := adminCard(t, s)
	if card.Plus == nil || card.Plus.Source != plusSourcePurchase {
		t.Fatalf("источник %q, ожидал покупку: %+v", card.Plus.Source, card.Plus)
	}
	// Но сам подарок из карточки не исчезает — иначе его нечем было бы отозвать.
	if card.Plus.Grant == nil {
		t.Fatal("подарок, перекрытый покупкой, пропал из карточки")
	}
}

// Подарок виден и у человека из списка в окружении — там он тоже перекрыт.
func TestAdminPlusGrantVisibleUnderComp(t *testing.T) {
	s, grants, subs := adminGrantServer(t, false)
	ent := service.NewEntitlements(subs, service.EntitlementsConfig{
		FreeQuota: 5, PlusQuota: service.UnlimitedQuota, LegacyQuota: 50,
		CompUserIds: []int{testUser1.ID},
	})
	ent.SetGrants(grants)
	s.SetEntitlements(ent)
	grants.give(testUser1.ID, time.Now().UTC().Add(30*24*time.Hour))

	card := adminCard(t, s)
	if card.Plus == nil || card.Plus.Source != plusSourceComp {
		t.Fatalf("источник %q, ожидал список в окружении", card.Plus.Source)
	}
	if card.Plus.Grant == nil {
		t.Fatal("подарок под comp-аккаунтом пропал из карточки")
	}
}

// Подарок виден и у бесплатного, у которого он ещё не начал действовать в
// резолве по любой причине: блок grant не зависит от тарифа.
func TestAdminPlusGrantShownForFreeUserToo(t *testing.T) {
	s, grants, _ := adminGrantServer(t, false)
	grants.give(testUser1.ID, time.Now().UTC().Add(30*24*time.Hour))

	card := adminCard(t, s)
	if card.Plus == nil || card.Plus.Grant == nil {
		t.Fatalf("подарок не показан: %+v", card.Plus)
	}
}

// Битое тело DELETE не приводит к отзыву.
//
// Первое разрушающее действие: ошибка клиента не должна всё равно снимать Plus.
func TestAdminRevokePlusRejectsMalformedBody(t *testing.T) {
	s, grants, _ := adminGrantServer(t, false)
	grants.give(testUser1.ID, time.Now().UTC().Add(30*24*time.Hour))

	rec := doAdminMethod(t, s, http.MethodDelete,
		"/admin/users/"+strconv.Itoa(testUser1.ID)+"/plus", testAdminToken, `{"reason":`)
	assertErrorCode(t, rec, http.StatusBadRequest, "validation")

	if g := grants.byUser[testUser1.ID]; g == nil || g.RevokedAt != nil {
		t.Fatalf("битое тело всё равно отозвало Plus: %+v", g)
	}
}

// Отказ базы — 500, а не 404: иначе недоступность mongo выглядит как опечатка
// в номере человека, и в панели ищут несуществующую ошибку.
func TestAdminGrantPlusDatabaseFailureIsNotNotFound(t *testing.T) {
	users := newFakeUserRepo(testUser1, testUser2)
	users.findErr = errors.New("mongo недоступна")
	s := newTestServer(Config{AdminToken: testAdminToken}, users, newFakeRoomRepo(newTestRoom()))
	s.SetPlusGrants(newFakeGrantStore())

	rec := doAdminMethod(t, s, http.MethodPost,
		"/admin/users/"+strconv.Itoa(testUser1.ID)+"/plus", testAdminToken,
		grantBody(time.Now().UTC().Add(24*time.Hour), ""))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("код %d, ожидал 500: %s", rec.Code, rec.Body.String())
	}
}
