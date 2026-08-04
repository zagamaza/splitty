package rest

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
)

// Возврат Telegram Login Widget в нативное приложение.
//
// Поток целиком (клиент — ios/Splitty/Core/TelegramWebAuth.swift):
//
//	1. приложение открывает ASWebAuthenticationSession на
//	   https://oauth.telegram.org/auth?bot_id=…&origin=<PUBLIC_BASE_URL>
//	   &return_to=<PUBLIC_BASE_URL>/tg-callback?native=1
//	2. человек подтверждает вход, Telegram редиректит на эту страницу и
//	   кладёт подписанный payload в tgAuthResult (query ИЛИ fragment)
//	3. страница уводит браузер в splitty://tg-callback?tgAuthResult=…
//	4. сессия ловит кастомную схему, приложение декодирует base64 и шлёт
//	   поля в POST /api/v1/auth/telegram — там подпись и свежесть проверяются
//	   как у обычного виджета (checkTelegramHash, auth.go)
//
// ⚠️ Страница сознательно НЕ разбирает payload и ничего не проверяет: подпись
// валидирует бэкенд на /auth/telegram, у которого есть TG_TOKEN. Здесь только
// перекладывание значения из одного транспорта в другой.
//
// ⚠️ origin и домен бота обязаны совпадать: Telegram отдаёт виджет только на
// домен, привязанный к боту через BotFather /setdomain. Без этого шага
// oauth.telegram.org ответит «Bot domain invalid» ещё до нашей страницы.

// tgCallbackTemplate — самодостаточная страница без внешних ресурсов.
//
// Fragment (#tgAuthResult=…) браузер на сервер не отправляет, поэтому забрать
// его можно только скриптом на клиенте — отсюда JS, а не серверный редирект.
// Ссылка-фолбэк нужна, когда автоматический переход заблокирован (в некоторых
// браузерах location.replace в кастомную схему без жеста пользователя молчит).
var tgCallbackTemplate = template.Must(template.New("tgcb").Parse(`<!doctype html>
<html lang="ru"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Splitty</title>
<style>
  body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
       font:16px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;
       background:#F6F7F9;color:#101828}
  @media (prefers-color-scheme: dark){body{background:#0C0F13;color:#E6E9EE}}
  .box{text-align:center;padding:24px;max-width:22rem}
  a{display:inline-block;margin-top:16px;padding:12px 20px;border-radius:12px;
    background:#0E9F6E;color:#fff;text-decoration:none;font-weight:600}
</style>
</head><body><div class="box">
  <p id="msg">Возвращаемся в приложение…</p>
  <a id="open" href="#" hidden>Открыть Splitty</a>
</div>
<script>
(function () {
  var q = new URLSearchParams(location.search);
  var h = new URLSearchParams(location.hash.slice(1));
  var res = q.get("tgAuthResult") || h.get("tgAuthResult");
  if (!res) {
    document.getElementById("msg").textContent = "Telegram не передал результат входа. Вернитесь в приложение и попробуйте ещё раз.";
    return;
  }
  var deep = {{.Scheme}} + "://tg-callback?tgAuthResult=" + encodeURIComponent(res);
  var a = document.getElementById("open");
  a.href = deep;
  a.hidden = false;
  location.replace(deep);
})();
</script>
</body></html>`))

// handleTelegramCallback — страница возврата из Telegram Login Widget.
// Выключена вместе с остальным диплинк-блоком, пока PUBLIC_BASE_URL пуст:
// без домена Telegram сюда всё равно не приведёт.
func (s *Server) handleTelegramCallback(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PublicBaseUrl == "" {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
		return
	}

	w.Header().Set("X-Robots-Tag", "noindex")
	// payload одноразовый и подписан на конкретный auth_date — кешировать нечего
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Схема — константа приложения, не пользовательский ввод: template.URL
	// здесь не нужен, значение уходит в JS-строку через {{.Scheme}}
	if err := tgCallbackTemplate.Execute(w, struct{ Scheme string }{Scheme: appScheme}); err != nil {
		// заголовки уже ушли — только лог, менять статус поздно
		log.Error().Err(err).Msg("cannot render tg-callback page")
	}
}

// handleTelegramAuthStart — вход в виджет: 302 на oauth.telegram.org.
//
// ⚠️ Ссылку собирает СЕРВЕР, а не клиент, и это не педантизм. Telegram сверяет
// origin с доменом, привязанным к боту, а bot_id обязан принадлежать тому же
// TG_TOKEN, которым потом проверяется подпись. Клиент не знает ни того, ни
// другого: его baseURL может указывать на http-IP или на локальный сервер, и
// собранный им origin не совпал бы с доменом бота. Поэтому приложение просто
// открывает <PUBLIC_BASE_URL>/tg-auth и ни о чём не думает.
func (s *Server) handleTelegramAuthStart(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimRight(s.cfg.PublicBaseUrl, "/")
	botID := telegramBotID(s.cfg.TgToken)
	if base == "" || botID == "" {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
		return
	}

	q := url.Values{}
	q.Set("bot_id", botID)
	q.Set("origin", base)
	q.Set("return_to", base+"/tg-callback?native=1")
	q.Set("embed", "0")
	q.Set("request_access", "write")

	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "https://oauth.telegram.org/auth?"+q.Encode(), http.StatusFound)
}

// telegramBotID достаёт числовой id бота из токена вида "<id>:<secret>".
// Сам id не секрет (он же в ссылке t.me), секретна только часть после
// двоеточия — её мы наружу не отдаём никогда.
func telegramBotID(token string) string {
	id, _, ok := strings.Cut(token, ":")
	if !ok || id == "" {
		return ""
	}
	return id
}
