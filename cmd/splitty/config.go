package main

import "github.com/caarlos0/env/v6"

type config struct {
	Listen   string `env:"LISTEN" envDefault:"localhost:7171"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"debug"`
	LogFmt   string `env:"LOG_FMT" envDefault:"console"`

	DbAddr          string   `env:"DB_HOST" envDefault:"mongodb://localhost:27017/"`
	DbName          string   `env:"DB_NAME" envDefault:"splitty"`
	TgToken         string   `env:"TG_TOKEN" envDefault:""`
	SuperUsers      []string `env:"SUPER_USER" envSeparator:":" envDefault:"mazanur:zagirnur"`
	TgDebug         bool     `env:"TG_DEBUG" envDefault:"false"`
	DefaultLanguage string   `env:"DEFAULT_LANGUAGE" envDefault:"en"`

	DailyExpensesUrl   string `env:"DAILY_EXPENSES_URL" envDefault:"http://pet.zagirnur.dev:19090/from-splitty"`
	DailyExpensesUsers []int  `env:"DAILY_EXPENSES_USERS" envSeparator:":" envDefault:"147181773:369575379:172261383:304898122:360624984:373160631"`

	// ApiJwtSecret намеренно без envDefault: вшитый в исходники секрет позволял бы
	// подделывать JWT любому, кто читал репозиторий. Политика: пустой секрет —
	// фатальная ошибка старта; исключение — API_DEV_AUTH=true, тогда генерируется
	// случайный эфемерный секрет (см. resolveJwtSecret в main.go)
	ApiJwtSecret string `env:"API_JWT_SECRET" envDefault:""`
	ApiDevAuth   bool   `env:"API_DEV_AUTH" envDefault:"false"`
	// Многоразовый код входа для ревьюеров App Store + id демо-аккаунта
	ReviewLoginCode string `env:"REVIEW_LOGIN_CODE" envDefault:""`
	ReviewUserId    int    `env:"REVIEW_USER_ID" envDefault:"0"`

	// GoogleClientIds — OAuth client id приложений (iOS, Android, web), которым
	// Google выпускает ID-токены; любой из них принимается как aud. Разделитель
	// ":", потому что client id содержит точки и дефисы, но не двоеточия.
	// Пусто — вход через Google выключен и POST /api/v1/auth/google отдаёт 503.
	// ВНИМАНИЕ: envDefault:"" со сплитом даёт [""], а не пустой срез — пустые
	// элементы обязательно отфильтровать (см. nonEmptyValues в main.go)
	GoogleClientIds []string `env:"GOOGLE_CLIENT_IDS" envSeparator:":" envDefault:""`

	// AppleClientIds — client id, которым Apple выпускает ID-токены: bundle id
	// приложения и/или Services ID. Пусто — вход через Apple выключен и
	// POST /api/v1/auth/apple отдаёт 503. Пустые элементы фильтруются так же,
	// как у Google (см. nonEmptyValues в main.go)
	AppleClientIds []string `env:"APPLE_CLIENT_IDS" envSeparator:":" envDefault:""`

	// AppleTeamId/AppleKeyId/ApplePrivateKey — доступ к token-эндпоинтам Apple
	// (обмен authorizationCode и отзыв токенов при удалении аккаунта, требование
	// Guideline 5.1.1(v)). ApplePrivateKey — СОДЕРЖИМОЕ файла .p8, а не путь:
	// ключ приезжает секретом окружения и в репозиторий не попадает НИКОГДА.
	// Пустой ключ — обмен выключен, вход через Apple при этом работает
	AppleTeamId     string `env:"APPLE_TEAM_ID" envDefault:""`
	AppleKeyId      string `env:"APPLE_KEY_ID" envDefault:""`
	ApplePrivateKey string `env:"APPLE_PRIVATE_KEY" envDefault:""`

	// Диплинк-вход в группу: associated files (.well-known) + публичная
	// страница приглашения /join/{roomId}.
	//
	// PublicBaseUrl — https-адрес, на котором стоит сервер; он же выключатель
	// всей фичи: пусто — все три маршрута отдают 404. Домен на момент написания
	// ещё не куплен, поэтому бэкенд катится с пустым значением и включается
	// одной переменной, когда домен появится.
	// IosAppId — <TeamID>.<bundle id>, например K8922Y6R3M.com.zagir.splitty.
	// AndroidPackage — имя пакета, например com.zagir.splitty.
	// AndroidCertSha256 — SHA-256 отпечатки подписи для assetlinks.json.
	// ВНИМАНИЕ: разделитель здесь ",", а не привычный ":" — сам отпечаток
	// состоит из байтов через двоеточие (E6:8C:8C:…), и ":" разорвал бы его.
	// Отпечатков может быть несколько: Play App Signing и локальный debug-ключ.
	// IosStoreUrl — карточка в App Store (её числовой id появляется только
	// после первой публикации, вывести его из bundle id нельзя).
	// AppScheme — схема кнопки «Открыть в приложении»: <scheme>://join/<roomId>
	PublicBaseUrl     string   `env:"PUBLIC_BASE_URL" envDefault:""`
	IosAppId          string   `env:"IOS_APP_ID" envDefault:""`
	AndroidPackage    string   `env:"ANDROID_PACKAGE" envDefault:""`
	AndroidCertSha256 []string `env:"ANDROID_CERT_SHA256" envSeparator:"," envDefault:""`
	IosStoreUrl       string   `env:"IOS_STORE_URL" envDefault:""`
	AppScheme         string   `env:"APP_SCHEME" envDefault:"splitty"`

	// AI-распознавание расхода (голос/фото чека). Пустой ключ отключает фичу
	// (эндпоинт /parse вернёт 503), остальной сервер работает как раньше.
	GeminiApiKey      string `env:"GEMINI_API_KEY" envDefault:""`
	GeminiModel       string `env:"GEMINI_MODEL" envDefault:"gemini-3.1-flash-lite"`
	AiParseRatePerMin int    `env:"AI_PARSE_RATE_PER_MIN" envDefault:"5"`
	AiParseDailyQuota int    `env:"AI_PARSE_DAILY_QUOTA" envDefault:"50"`
	AiMaxBodyBytes    int64  `env:"AI_MAX_BODY_BYTES" envDefault:"15728640"` // 15 МБ

	// FCM push. Путь к service-account JSON Firebase Admin (см. firebase-service-
	// account.json, в .gitignore). Пусто — пуши выключены (NoopSender), сервер
	// работает как раньше.
	FirebaseCredentialsFile string `env:"FIREBASE_CREDENTIALS_FILE" envDefault:""`
}

func initConfig() (*config, error) {
	cfg := &config{}

	if err := env.Parse(cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}
