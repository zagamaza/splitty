package rest

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/rs/zerolog/log"
)

// Вход через Telegram Login Widget для нативных клиентов:
// /tg-auth → oauth.telegram.org → /tg-callback → splitty://tg-callback → POST /auth/telegram.
// Подпись payload проверяет /auth/telegram (checkTelegramHash), здесь её не трогаем.

// Fragment (#tgAuthResult=…) на сервер не приходит, поэтому значение забирает скрипт.
// Ссылка-фолбэк — на случай, когда переход в кастомную схему без жеста заблокирован.
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

func (s *Server) handleTelegramCallback(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PublicBaseUrl == "" {
		writeError(w, http.StatusNotFound, "not_found", "не найдено")
		return
	}

	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := tgCallbackTemplate.Execute(w, struct{ Scheme string }{Scheme: appScheme}); err != nil {
		log.Error().Err(err).Msg("cannot render tg-callback page")
	}
}

// handleTelegramAuthStart — 302 на виджет Telegram.
//
// Ссылку собирает сервер, а не клиент: Telegram сверяет origin с доменом бота, а
// baseURL клиента может указывать куда угодно.
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

// telegramBotID — числовой id из токена "<id>:<secret>"; сам id не секрет.
func telegramBotID(token string) string {
	id, _, ok := strings.Cut(token, ":")
	if !ok || id == "" {
		return ""
	}
	return id
}
