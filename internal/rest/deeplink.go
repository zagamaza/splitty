package rest

// Публичная часть диплинк-входа в группу: два associated file, по которым iOS и
// Android признают домен своим, и страница приглашения /join/{roomId}.
//
// Все три маршрута регистрируются БЕЗ s.auth, и это не упущение:
//   - .well-known-файлы читают операционные системы, никакой авторизации у них нет;
//   - страницу /join открывает браузер человека, у которого аккаунта ещё может
//     не быть — ради него диплинк и делается.
//
// Отсюда правила, которые нельзя ослаблять:
//   - страница отдаёт ТОЛЬКО название группы и код приглашения. Ни участников,
//     ни сумм, ни операций: ссылку пересылают в чаты, и всякий, кому она попала,
//     увидит ровно то, что видит адресат;
//   - несуществующая комната и комната, к которой у смотрящего нет отношения,
//     неразличимы — обе дают нейтральное «Приглашение не найдено»;
//   - название комнаты — пользовательский ввод, поэтому страница собирается
//     html/template (контекстное экранирование), а не конкатенацией строк;
//   - свой префикс троттлинга "join:": страница ходит в mongo на каждый вызов и
//     служит оракулом существования комнаты по ObjectID, а открывают её браузеры
//     за одним NAT толпой — общий ключ с /auth/code выжигал бы людям вход по коду.

import (
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	// joinPerIPPerMin — открытий страницы приглашения с одного адреса в минуту.
	// Порог заметно выше кодового (authCodePerIPPerMin): перебирать здесь нечего
	// — знание ObjectID комнаты и есть приглашение, — а за одним адресом сидит
	// целая офисная или домовая сеть, куда ссылку и переслали
	joinPerIPPerMin = 60

	// defaultAppScheme — кастомная схема кнопки «Открыть в приложении».
	// Universal link на самого себя тут не годится: тап по ссылке НА ТОТ ЖЕ
	// домен iOS в приложение не уводит, он остаётся в браузере
	defaultAppScheme = "splitty"

	// playStorePrefix — карточка приложения в Google Play собирается из package
	// name, отдельной настройки для неё не нужно
	playStorePrefix = "https://play.google.com/store/apps/details?id="
)

// handleAppleAppSiteAssociation GET /.well-known/apple-app-site-association —
// файл, по которому iOS связывает домен с приложением.
//
// Content-Type строго application/json и никаких редиректов: iOS скачивает файл
// сам (через CDN Apple) и молча отвергает и редирект, и чужой тип — universal
// links после этого не работают, а диагностики у разработчика почти нет
func (s *Server) handleAppleAppSiteAssociation(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.PublicBaseUrl == "" || s.cfg.IosAppId == "" {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
		return
	}

	// Формат современный (appIDs + components); legacy-ветку apps/paths не
	// добавляем — она нужна была iOS 12 и старше
	body := map[string]any{
		"applinks": map[string]any{
			"details": []any{
				map[string]any{
					"appIDs": []string{s.cfg.IosAppId},
					"components": []any{
						map[string]any{
							"/":       "/join/*",
							"comment": "приглашение в группу",
						},
					},
				},
			},
		},
	}
	writeAssociatedFile(w, body)
}

// handleAssetLinks GET /.well-known/assetlinks.json — файл Digital Asset Links,
// по которому Android верифицирует app links (autoVerify в манифесте)
func (s *Server) handleAssetLinks(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.PublicBaseUrl == "" || s.cfg.AndroidPackage == "" || len(s.cfg.AndroidCertSha256) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
		return
	}

	// Отпечатков может быть несколько: сертификат Play App Signing и локальный
	// debug-ключ — иначе app links не верифицируются в отладочной сборке
	body := []any{
		map[string]any{
			"relation": []string{"delegate_permission/common.handle_all_urls"},
			"target": map[string]any{
				"namespace":                "android_app",
				"package_name":             s.cfg.AndroidPackage,
				"sha256_cert_fingerprints": s.cfg.AndroidCertSha256,
			},
		},
	}
	writeAssociatedFile(w, body)
}

// handleJoinPage GET /join/{roomId} — публичная страница приглашения
func (s *Server) handleJoinPage(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PublicBaseUrl == "" {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
		return
	}
	if !s.authThrottle.allow("join:"+clientIP(r), joinPerIPPerMin) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много запросов, попробуйте позже")
		return
	}

	roomId := r.PathValue("roomId")
	// Проверка формата ДО похода в базу: мусорный путь не должен стоить запроса
	if _, err := primitive.ObjectIDFromHex(roomId); err != nil {
		s.writeJoinPage(w, http.StatusNotFound, joinPageData{})
		return
	}

	room, err := s.roomRepo.FindById(r.Context(), roomId)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// нейтральный ответ: «нет такой комнаты» и «эта комната не для вас»
			// обязаны выглядеть одинаково
			s.writeJoinPage(w, http.StatusNotFound, joinPageData{})
			return
		}
		log.Error().Err(err).Msgf("cannot find room for join page: %s", roomId)
		s.writeJoinPage(w, http.StatusInternalServerError, joinPageData{Failed: true})
		return
	}

	s.writeJoinPage(w, http.StatusOK, joinPageData{
		Found:    true,
		RoomName: room.Name,
		RoomCode: roomId,
		// template.URL, потому что html/template не знает нашу схему и заменяет
		// href на #ZgotmplZ. Обходить экранирование безопасно ровно потому, что
		// в строке нет ни байта пользовательского ввода: схема просеяна
		// appScheme(), а roomId уже признан валидным ObjectID (только hex)
		OpenURL:         template.URL(s.appScheme() + "://join/" + roomId),
		IosStoreURL:     s.cfg.IosStoreUrl,
		AndroidStoreURL: s.playStoreURL(),
	})
}

// appScheme — схема кастомного диплинка приложения.
//
// Значение просеивается по алфавиту схем из RFC 3986: оно приезжает из
// окружения и подставляется в href в обход экранирования (см. template.URL
// выше), поэтому «настройка кривая» не должна превращаться в инъекцию.
// Всё неожиданное — молча заменяется дефолтом
func (s *Server) appScheme() string {
	scheme := s.cfg.AppScheme
	if scheme == "" {
		return defaultAppScheme
	}
	for i, c := range scheme {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			log.Warn().Msgf("APP_SCHEME %q не похож на схему url, используется %q", scheme, defaultAppScheme)
			return defaultAppScheme
		}
	}
	return scheme
}

// playStoreURL — карточка в Google Play, собранная из package name; пусто, если
// package не задан (тогда ссылка на стор просто не рисуется)
func (s *Server) playStoreURL() string {
	if s.cfg.AndroidPackage == "" {
		return ""
	}
	return playStorePrefix + s.cfg.AndroidPackage
}

// joinPageData — данные шаблона страницы приглашения. RoomName приходит от
// пользователя, поэтому подстановка только через html/template
type joinPageData struct {
	Found    bool
	Failed   bool
	RoomName string
	RoomCode string
	// OpenURL — template.URL: схема кастомная, и html/template иначе вырезает
	// её как небезопасную. Собирается только из просеянной схемы и hex-id
	OpenURL         template.URL
	IosStoreURL     string
	AndroidStoreURL string
}

// writeJoinPage рендерит страницу с обязательными заголовками.
//
// X-Robots-Tag: noindex — ссылка-приглашение содержит название группы, и в
// поисковую выдачу оно попадать не должно; Cache-Control: no-store — по тому же
// адресу название комнаты не должно оседать у прокси
func (s *Server) writeJoinPage(w http.ResponseWriter, status int, data joinPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := joinPageTemplate.Execute(w, data); err != nil {
		log.Error().Err(err).Msg("cannot render join page")
	}
}

// writeAssociatedFile отдаёт associated file именно как application/json, без
// charset и без обёрток: и iOS, и Android читают эти файлы машинно
func writeAssociatedFile(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Error().Err(err).Msg("cannot write associated file")
	}
}

// joinPageTemplate — вся страница целиком. Один шаблон на все исходы: обвязка
// общая, различается только карточка. Стили и скрипт inline — внешних ресурсов
// у публичной страницы быть не должно, её открывают по ссылке из мессенджера
var joinPageTemplate = template.Must(template.New("join").Parse(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{if .Found}}Приглашение в группу — Splitty{{else}}Приглашение не найдено — Splitty{{end}}</title>
<style>
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
body {
  margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
  padding: 24px; background: #f4f4f7; color: #16161a;
  font: 16px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
}
.card { width: 100%; max-width: 380px; background: #fff; border-radius: 20px; padding: 28px 24px; box-shadow: 0 10px 40px rgba(0,0,0,.08); }
h1 { margin: 0 0 4px; font-size: 22px; line-height: 1.25; overflow-wrap: anywhere; }
.muted { margin: 0 0 20px; color: #6b6b76; font-size: 14px; }
.code { display: flex; align-items: center; gap: 8px; margin-bottom: 20px; }
code { flex: 1; padding: 12px 14px; border-radius: 12px; background: #f0f0f4; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 14px; overflow-wrap: anywhere; }
button, .btn {
  display: block; width: 100%; padding: 14px 16px; border: 0; border-radius: 14px;
  font: inherit; font-weight: 600; text-align: center; text-decoration: none; cursor: pointer;
}
.copy { width: auto; padding: 12px 14px; background: #e8e8ef; color: #16161a; }
.primary { background: #4c6ef5; color: #fff; margin-bottom: 12px; }
.stores { display: flex; gap: 10px; }
.stores .btn { background: #f0f0f4; color: #16161a; font-weight: 500; font-size: 14px; }
@media (prefers-color-scheme: dark) {
  body { background: #101014; color: #f4f4f7; }
  .card { background: #1c1c22; box-shadow: none; }
  code, .copy, .stores .btn { background: #26262e; color: #f4f4f7; }
  .muted { color: #9a9aa6; }
}
</style>
</head>
<body>
<main class="card">
{{if .Found}}
  <h1>{{.RoomName}}</h1>
  <p class="muted">Вас приглашают в группу в Splitty</p>
  <div class="code">
    <code id="room-code">{{.RoomCode}}</code>
    <button class="copy" id="copy" type="button">Скопировать</button>
  </div>
  <a class="btn primary" href="{{.OpenURL}}">Открыть в приложении</a>
  <div class="stores">
    {{if .IosStoreURL}}<a class="btn" href="{{.IosStoreURL}}">App Store</a>{{end}}
    {{if .AndroidStoreURL}}<a class="btn" href="{{.AndroidStoreURL}}">Google Play</a>{{end}}
  </div>
  <script>
  (function () {
    var button = document.getElementById('copy');
    var code = document.getElementById('room-code');
    if (!button || !code) { return; }
    button.addEventListener('click', function () {
      var text = code.textContent;
      var done = function () { button.textContent = 'Скопировано'; };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done, function () {});
        return;
      }
      var area = document.createElement('textarea');
      area.value = text;
      document.body.appendChild(area);
      area.select();
      try { document.execCommand('copy'); done(); } catch (e) {}
      document.body.removeChild(area);
    });
  })();
  </script>
{{else if .Failed}}
  <h1>Что-то пошло не так</h1>
  <p class="muted">Попробуйте открыть ссылку ещё раз чуть позже.</p>
{{else}}
  <h1>Приглашение не найдено</h1>
  <p class="muted">Ссылка устарела или введена неверно. Попросите отправителя прислать её заново.</p>
{{end}}
</main>
</body>
</html>
`))
