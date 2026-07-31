package rest

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	testIosAppId       = "K8922Y6R3M.com.zagir.splitty"
	testAndroidPackage = "com.zagir.splitty"
	testCertSha256     = "E6:8C:8C:AF:20:18:20:2B:E3:93:BF:BE:AE:B9:DA:E6:AB:E7:BD:AE:AA:39:D2:20:9D:24:E4:75:B4:ED:E7:D0"
)

// deeplinkConfig — конфиг с включённым диплинком
func deeplinkConfig() Config {
	return Config{
		PublicBaseUrl:     "https://splitty.example",
		IosAppId:          testIosAppId,
		AndroidPackage:    testAndroidPackage,
		AndroidCertSha256: []string{testCertSha256},
		IosStoreUrl:       "https://apps.apple.com/app/id123456789",
	}
}

// AASA обязан отдаваться как application/json и без редиректов: iOS скачивает
// файл машинно и молча отвергает и чужой Content-Type, и 3xx
func TestAppleAppSiteAssociation(t *testing.T) {
	s := newTestServer(deeplinkConfig(), newFakeUserRepo(), newFakeRoomRepo())

	rec := doRequest(t, s, http.MethodGet, "/.well-known/apple-app-site-association", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (редирект или ошибка — iOS файл не примет), body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Applinks struct {
			Details []struct {
				AppIDs     []string         `json:"appIDs"`
				Components []map[string]any `json:"components"`
			} `json:"details"`
		} `json:"applinks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("cannot parse aasa: %v, body: %s", err, rec.Body.String())
	}
	if len(body.Applinks.Details) != 1 {
		t.Fatalf("details = %d, want 1", len(body.Applinks.Details))
	}
	d := body.Applinks.Details[0]
	if len(d.AppIDs) != 1 || d.AppIDs[0] != testIosAppId {
		t.Errorf("appIDs = %v, want [%s]", d.AppIDs, testIosAppId)
	}
	if len(d.Components) != 1 || d.Components[0]["/"] != "/join/*" {
		t.Errorf("components = %v, want путь /join/*", d.Components)
	}
}

// Без IOS_APP_ID файл отдавать нельзя: пустой appID хуже отсутствия файла —
// iOS закеширует негодную ассоциацию домена
func TestAppleAppSiteAssociationWithoutAppId(t *testing.T) {
	cfg := deeplinkConfig()
	cfg.IosAppId = ""
	s := newTestServer(cfg, newFakeUserRepo(), newFakeRoomRepo())

	rec := doRequest(t, s, http.MethodGet, "/.well-known/apple-app-site-association", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAssetLinks(t *testing.T) {
	s := newTestServer(deeplinkConfig(), newFakeUserRepo(), newFakeRoomRepo())

	rec := doRequest(t, s, http.MethodGet, "/.well-known/assetlinks.json", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body []struct {
		Relation []string `json:"relation"`
		Target   struct {
			Namespace    string   `json:"namespace"`
			PackageName  string   `json:"package_name"`
			Fingerprints []string `json:"sha256_cert_fingerprints"`
		} `json:"target"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("cannot parse assetlinks: %v, body: %s", err, rec.Body.String())
	}
	if len(body) != 1 {
		t.Fatalf("statements = %d, want 1", len(body))
	}
	st := body[0]
	if len(st.Relation) != 1 || st.Relation[0] != "delegate_permission/common.handle_all_urls" {
		t.Errorf("relation = %v", st.Relation)
	}
	if st.Target.Namespace != "android_app" || st.Target.PackageName != testAndroidPackage {
		t.Errorf("target = %+v, want package %s", st.Target, testAndroidPackage)
	}
	if len(st.Target.Fingerprints) != 1 || st.Target.Fingerprints[0] != testCertSha256 {
		t.Errorf("fingerprints = %v, want [%s]", st.Target.Fingerprints, testCertSha256)
	}
}

// Отпечатков может быть несколько (Play App Signing + debug-ключ), иначе app
// links не верифицируются в отладочной сборке
func TestAssetLinksMultipleFingerprints(t *testing.T) {
	cfg := deeplinkConfig()
	second := "AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99"
	cfg.AndroidCertSha256 = []string{testCertSha256, second}
	s := newTestServer(cfg, newFakeUserRepo(), newFakeRoomRepo())

	rec := doRequest(t, s, http.MethodGet, "/.well-known/assetlinks.json", "", "")
	if !strings.Contains(rec.Body.String(), second) {
		t.Errorf("второй отпечаток не попал в файл: %s", rec.Body.String())
	}
}

func TestAssetLinksWithoutPackageOrFingerprint(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
	}{
		{name: "без package", mut: func(c *Config) { c.AndroidPackage = "" }},
		{name: "без отпечатков", mut: func(c *Config) { c.AndroidCertSha256 = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := deeplinkConfig()
			tt.mut(&cfg)
			s := newTestServer(cfg, newFakeUserRepo(), newFakeRoomRepo())

			rec := doRequest(t, s, http.MethodGet, "/.well-known/assetlinks.json", "", "")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
		})
	}
}

// Страница приглашения: код комнаты, кнопка открытия в приложении, ссылки на
// сторы — и обязательные заголовки. Авторизации не требуется по определению
func TestJoinPageRendersCode(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(deeplinkConfig(), newFakeUserRepo(), newFakeRoomRepo(room))

	rec := doRequest(t, s, http.MethodGet, "/join/"+room.ID.Hex(), "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Errorf("X-Robots-Tag = %q, want noindex", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	body := rec.Body.String()
	for _, want := range []string{
		room.ID.Hex(),                        // код приглашения
		room.Name,                            // название группы
		"splitty://join/" + room.ID.Hex(),    // кнопка «Открыть в приложении»
		"Скопировать",                        // кнопка копирования
		playStorePrefix + testAndroidPackage, // Google Play
		"https://apps.apple.com/app/id123456789",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("страница не содержит %q", want)
		}
	}
}

// Публичная страница не должна раскрывать НИЧЕГО, кроме названия и кода:
// ссылку пересылают в чаты, и её видит всякий, кому она попала
func TestJoinPageHidesPrivateData(t *testing.T) {
	// Сентинелы намеренно непохожи ни на что в шаблоне и в настройках: имя
	// "zagir" из фикстур, например, встречается в package name приложения
	secretMember := api.User{ID: 77, Username: "sentinel-username", DisplayName: "Сентинел Участник"}
	room := &api.Room{
		ID:      primitive.NewObjectID(),
		Name:    "Поездка",
		Members: &[]api.User{secretMember},
		Operations: &[]api.Operation{{
			ID:          primitive.NewObjectID(),
			Description: "Сентинел Расход",
			Sum:         987654,
			Donor:       &secretMember,
		}},
	}
	s := newTestServer(deeplinkConfig(), newFakeUserRepo(), newFakeRoomRepo(room))

	body := doRequest(t, s, http.MethodGet, "/join/"+room.ID.Hex(), "", "").Body.String()
	for _, leaked := range []string{
		secretMember.DisplayName, secretMember.Username, // участники
		"Сентинел Расход", "987654", // описание и сумма операции
	} {
		if strings.Contains(body, leaked) {
			t.Errorf("страница раскрывает приватные данные: %q", leaked)
		}
	}
}

// Название комнаты — пользовательский ввод: без экранирования страница
// приглашения превращается в XSS, который автор комнаты рассылает сам
func TestJoinPageEscapesRoomName(t *testing.T) {
	room := newTestRoom()
	room.Name = `<script>alert(1)</script>`
	s := newTestServer(deeplinkConfig(), newFakeUserRepo(), newFakeRoomRepo(room))

	body := doRequest(t, s, http.MethodGet, "/join/"+room.ID.Hex(), "", "").Body.String()
	if strings.Contains(body, "<script>alert(1)") {
		t.Fatalf("имя комнаты вставлено без экранирования: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("экранированного имени нет в странице: %s", body)
	}
}

// Несуществующая комната, чужая комната и мусорный id обязаны выглядеть
// ОДИНАКОВО: иначе страница становится оракулом существования комнаты
func TestJoinPageUnknownRoomIsNeutral(t *testing.T) {
	s := newTestServer(deeplinkConfig(), newFakeUserRepo(), newFakeRoomRepo())

	missing := doRequest(t, s, http.MethodGet, "/join/"+primitive.NewObjectID().Hex(), "", "")
	garbage := doRequest(t, s, http.MethodGet, "/join/not-an-object-id", "", "")

	for name, rec := range map[string]int{"несуществующая": missing.Code, "мусор": garbage.Code} {
		if rec != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, rec)
		}
	}
	if missing.Body.String() != garbage.Body.String() {
		t.Error("несуществующая комната и мусорный id отдают РАЗНЫЕ страницы — это оракул существования комнаты")
	}
	if !strings.Contains(missing.Body.String(), "Приглашение не найдено") {
		t.Errorf("нейтральный текст не показан: %s", missing.Body.String())
	}
}

// До покупки домена фича выключена целиком — это и есть причина, по которой её
// безопасно мерджить заранее
func TestDeeplinkRoutesDisabledWithoutPublicBaseUrl(t *testing.T) {
	room := newTestRoom()
	cfg := deeplinkConfig()
	cfg.PublicBaseUrl = ""
	s := newTestServer(cfg, newFakeUserRepo(), newFakeRoomRepo(room))

	for _, path := range []string{
		"/.well-known/apple-app-site-association",
		"/.well-known/assetlinks.json",
		"/join/" + room.ID.Hex(),
	} {
		rec := doRequest(t, s, http.MethodGet, path, "", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
	}
}

// Своё окно троттлинга с префиксом "join:": страница ходит в mongo на каждый
// вызов, но открывают её браузеры за одним NAT толпой — общий ключ с /auth/code
// выжигал бы людям вход по коду
func TestJoinPageThrottled(t *testing.T) {
	room := newTestRoom()
	s := newTestServer(deeplinkConfig(), newFakeUserRepo(), newFakeRoomRepo(room))
	target := "/join/" + room.ID.Hex()

	for i := 0; i < joinPerIPPerMin; i++ {
		if rec := doRequest(t, s, http.MethodGet, target, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("попытка %d: status = %d, want 200", i, rec.Code)
		}
	}
	rec := doRequest(t, s, http.MethodGet, target, "", "")
	assertErrorCode(t, rec, http.StatusTooManyRequests, "rate_limited")

	// бюджет входа по коду с того же адреса не тронут
	codeRec := doRequest(t, s, http.MethodPost, "/api/v1/auth/code", "", `{"code":"ABCDEF"}`)
	assertErrorCode(t, codeRec, http.StatusUnauthorized, "invalid_code")
}

// Ссылка на App Store появляется только когда она задана: числовой id
// приложения существует лишь после первой публикации
func TestJoinPageWithoutStoreLinks(t *testing.T) {
	room := &api.Room{ID: primitive.NewObjectID(), Name: "Поездка"}
	cfg := deeplinkConfig()
	cfg.IosStoreUrl = ""
	cfg.AndroidPackage = ""
	cfg.AndroidCertSha256 = nil
	s := newTestServer(cfg, newFakeUserRepo(), newFakeRoomRepo(room))

	body := doRequest(t, s, http.MethodGet, "/join/"+room.ID.Hex(), "", "").Body.String()
	if strings.Contains(body, "App Store") || strings.Contains(body, "Google Play") {
		t.Errorf("нарисованы ссылки на сторы без адресов: %s", body)
	}
	if !strings.Contains(body, "splitty://join/"+room.ID.Hex()) {
		t.Error("кнопка «Открыть в приложении» пропала вместе со сторами")
	}
}

// Схема диплинка настраиваемая, но подставляется в href в обход экранирования
// (иначе html/template вырезает незнакомую схему) — поэтому кривое значение из
// окружения обязано откатываться к дефолту, а не уезжать в страницу
func TestJoinPageScheme(t *testing.T) {
	tests := []struct {
		name   string
		scheme string
		want   string
	}{
		{name: "по умолчанию", scheme: "", want: "splitty"},
		{name: "из конфига", scheme: "splittydev", want: "splittydev"},
		{name: "с цифрой и дефисом", scheme: "splitty-2", want: "splitty-2"},
		{name: "пробел и кавычка", scheme: `x" onclick="alert(1)`, want: "splitty"},
		{name: "javascript-инъекция", scheme: "javascript:alert(1)//", want: "splitty"},
		{name: "начинается с цифры", scheme: "1splitty", want: "splitty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			room := &api.Room{ID: primitive.NewObjectID(), Name: "Поездка"}
			cfg := deeplinkConfig()
			cfg.AppScheme = tt.scheme
			s := newTestServer(cfg, newFakeUserRepo(), newFakeRoomRepo(room))

			body := doRequest(t, s, http.MethodGet, "/join/"+room.ID.Hex(), "", "").Body.String()
			if !strings.Contains(body, `href="`+tt.want+"://join/"+room.ID.Hex()+`"`) {
				t.Errorf("ожидалась схема %q, страница: %s", tt.want, body)
			}
			if strings.Contains(body, "onclick") || strings.Contains(body, "javascript:") {
				t.Errorf("значение APP_SCHEME уехало в страницу как есть: %s", body)
			}
		})
	}
}
