package analytics

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// contractPath — документ с именами событий. Он источник правды для двух
// клиентов и для белого списка сервера.
func contractPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "docs", "analytics-events.md")
}

// contractRows разбирает таблицу событий: строки вида
// | `имя` | когда | параметры |
func contractRows(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(contractPath(t))
	if err != nil {
		t.Fatalf("не прочитал контракт: %v", err)
	}

	row := regexp.MustCompile("^\\|\\s*`([a-z0-9_]+)`\\s*\\|([^|]*)\\|(.*)\\|\\s*$")
	events := map[string]string{}
	inEvents := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "## События") {
			inEvents = true
			continue
		}
		if inEvents && strings.HasPrefix(line, "## ") {
			break
		}
		if !inEvents {
			continue
		}
		if m := row.FindStringSubmatch(line); m != nil {
			events[m[1]] = m[3]
		}
	}
	if len(events) == 0 {
		t.Fatal("в контракте не нашлось ни одного события — разбор таблицы сломан")
	}
	return events
}

// Имена событий уникальны. Повтор означал бы два разных смысла под одним
// именем, и в агрегатах они бы сложились в одно число.
func TestContractNamesAreUnique(t *testing.T) {
	raw, err := os.ReadFile(contractPath(t))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	row := regexp.MustCompile("^\\|\\s*`([a-z0-9_]+)`\\s*\\|")
	for _, line := range strings.Split(string(raw), "\n") {
		if m := row.FindStringSubmatch(line); m != nil {
			seen[m[1]]++
		}
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("событие %q объявлено %d раза", name, n)
		}
	}
}

// snake_case и разумная длина. Правило не косметическое: имена уезжают в ключи
// агрегатов, и разнобой в регистре разведёт одно действие на два события.
func TestContractNamesAreSnakeCase(t *testing.T) {
	ok := regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	for name := range contractRows(t) {
		if !ok.MatchString(name) {
			t.Errorf("имя %q не snake_case", name)
		}
		if len(name) > 40 {
			t.Errorf("имя %q длиннее 40 символов", name)
		}
	}
}

// У каждого параметра перечислены допустимые значения. Параметр без закрытого
// множества — это свободный текст, а свободный текст в аналитике означает
// две вещи сразу: агрегат, который нельзя сгруппировать, и утечку.
func TestContractParamsAreClosedSets(t *testing.T) {
	for name, params := range contractRows(t) {
		params = strings.TrimSpace(params)
		if params == "" || params == "—" {
			continue
		}
		if !strings.Contains(params, ":") {
			t.Errorf("%s: у параметра не назван ключ: %q", name, params)
			continue
		}
		if !strings.Contains(params, "`") {
			t.Errorf("%s: у параметра не перечислены значения: %q", name, params)
		}
	}
}

// contractParams разбирает колонку параметров: `ключ`: `знач` / `знач`.
func contractParams(cell string) map[string][]string {
	out := map[string][]string{}
	cell = strings.TrimSpace(cell)
	if cell == "" || cell == "—" {
		return out
	}
	key := regexp.MustCompile("`([a-z_]+)`\\s*:")
	value := regexp.MustCompile("`([a-z_0-9]+)`")
	for _, part := range strings.Split(cell, ";") {
		m := key.FindStringSubmatch(part)
		if m == nil {
			continue
		}
		rest := part[strings.Index(part, ":")+1:]
		var values []string
		for _, v := range value.FindAllStringSubmatch(rest, -1) {
			values = append(values, v[1])
		}
		out[m[1]] = values
	}
	return out
}

// Белый список и документ — одно и то же.
//
// Это и есть защита от дрейфа: имя события — проводной контракт, и опечатка в
// нём ничего не роняет. Она молча уводит шаг воронки в никуда, и видно это
// будет только по неправильному числу через месяц.
func TestWhitelistMatchesContract(t *testing.T) {
	doc := contractRows(t)

	for name := range doc {
		if _, ok := Events[name]; !ok {
			t.Errorf("событие %q описано в документе, но его нет в белом списке", name)
		}
	}
	for name := range Events {
		if _, ok := doc[name]; !ok {
			t.Errorf("событие %q есть в белом списке, но не описано в документе", name)
		}
	}

	for name, cell := range doc {
		event, ok := Events[name]
		if !ok {
			continue
		}
		want := contractParams(cell)
		for key, values := range want {
			got, ok := event.Params[key]
			if !ok {
				t.Errorf("%s: параметр %q описан в документе, но не разрешён кодом", name, key)
				continue
			}
			if strings.Join(got, ",") != strings.Join(values, ",") {
				t.Errorf("%s.%s: документ разрешает %v, код — %v", name, key, values, got)
			}
		}
		for key := range event.Params {
			if _, ok := want[key]; !ok {
				t.Errorf("%s: код разрешает параметр %q, которого нет в документе", name, key)
			}
		}
	}
}

// Незнакомое имя и значение вне множества отбиваются, отсутствующий параметр —
// нет: старая сборка может не уметь его заполнять, и терять из-за этого всё
// событие незачем.
func TestValidate(t *testing.T) {
	if err := Validate("app_open", map[string]string{"cold": "true"}); err != nil {
		t.Errorf("честное событие отбито: %v", err)
	}
	if err := Validate("app_open", nil); err != nil {
		t.Errorf("событие без параметров отбито: %v", err)
	}
	if Validate("никогда_такого_не_было", nil) == nil {
		t.Error("неизвестное имя прошло — так в агрегаты попадает мусор")
	}
	if Validate("app_open", map[string]string{"cold": "может быть"}) == nil {
		t.Error("значение вне множества прошло")
	}
	if Validate("app_open", map[string]string{"чужой": "true"}) == nil {
		t.Error("незнакомый ключ параметра прошёл")
	}
}
