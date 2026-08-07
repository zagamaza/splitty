# Splitty backend — сборка, тесты, деплой.
#
# GO: на CI/сервере берём `go` из PATH; локально (нет go в PATH) — падаем на
# установленный go1.23.5. go.mod объявляет `go 1.22`; GOTOOLCHAIN=local
# запрещает докачивать тулчейн из сети (локально её может не быть, на CI не нужна).
GO         ?= $(shell command -v go 2>/dev/null || echo $(HOME)/sdk/go1.23.5/bin/go)
GOENV      := GOTOOLCHAIN=local
BINARY     := ./bin/splitty

# --- Деплой (docker-compose на сервере) ---
# Пароль SSH вводится интерактивно и в Makefile НЕ хранится. Секреты живут в
# файле .env НА СЕРВЕРЕ рядом с docker-compose.yml (шаблон — .env.example).
SSH_HOST    ?= root@138.124.18.189
REMOTE_DIR  ?= /root/splitty                # каталог с docker-compose.yml на сервере (поправь под свой)
FIREBASE_SA := firebase-service-account.json

# --- Раздача сборок тестерам ---
# Оба стора грузятся по токену, без браузера. Ключи лежат вне git:
#   Android — android/play-sa.json (service account Play Developer API),
#   iOS     — ~/.appstoreconnect/private_keys/AuthKey_<KID>.p8.
ASC_KEY     ?= T6PMYHX4T7
ASC_ISSUER  ?= a30d44ef-0dc4-4c01-bb7b-7235968f61f8
ASC_P8      ?= $(HOME)/.appstoreconnect/private_keys/AuthKey_$(ASC_KEY).p8
PLAY_TRACK  ?= internal

.DEFAULT_GOAL := help

.PHONY: help wire build test vet tidy run docker-build push-secret deploy logs \
        android-publish ios-publish

help: ## список целей
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

wire: ## сгенерировать wire_gen.go (DI бота)
	wire gen ./cmd/splitty

build: ## собрать бинарь в bin/splitty
	$(GOENV) $(GO) build -o $(BINARY) ./cmd/splitty

test: ## юнит-тесты (все пакеты модуля — как в CI)
	$(GOENV) $(GO) test ./...

vet: ## go vet
	$(GOENV) $(GO) vet ./...

tidy: ## go mod tidy
	$(GOENV) $(GO) mod tidy

run: ## запустить локально (нужен .env в корне)
	$(GOENV) $(GO) run ./cmd/splitty

seed: ## демо-данные для UI-прогонов (бэкенд с API_DEV_AUTH=true уже поднят)
	python3 scripts/seed-local.py

docker-build: ## локальная сборка docker-образа (проверка Dockerfile)
	docker build -t splitty .

push-secret: ## залить firebase-креды на сервер (scp спросит пароль)
	scp $(FIREBASE_SA) $(SSH_HOST):$(REMOTE_DIR)/$(FIREBASE_SA)

deploy: ## сервер: git pull + пересборка контейнера (ssh спросит пароль)
	ssh $(SSH_HOST) 'cd $(REMOTE_DIR) && git pull && \
	  set -a && . ./.env && set +a && \
	  docker-compose up -d --build telegram-bot && \
	  docker-compose logs --tail=50 telegram-bot'

logs: ## логи контейнера на сервере (follow)
	ssh $(SSH_HOST) 'cd $(REMOTE_DIR) && docker-compose logs -f --tail=100 telegram-bot'

android-publish: ## Android → Google Play (PLAY_TRACK=internal по умолчанию)
	cd android && ./gradlew :app:publishReleaseBundle --track $(PLAY_TRACK)

ios-publish: ## iOS → TestFlight (архив, экспорт, загрузка)
	cd ios && rm -rf build/Splitty.xcarchive build/export && \
	  xcodegen generate && \
	  xcodebuild -project Splitty.xcodeproj -scheme Splitty -configuration Release \
	    -archivePath build/Splitty.xcarchive -destination 'generic/platform=iOS' \
	    -allowProvisioningUpdates \
	    -authenticationKeyPath "$(ASC_P8)" \
	    -authenticationKeyID "$(ASC_KEY)" \
	    -authenticationKeyIssuerID "$(ASC_ISSUER)" archive && \
	  xcodebuild -exportArchive -archivePath build/Splitty.xcarchive \
	    -exportOptionsPlist ExportOptions.plist -exportPath build/export \
	    -allowProvisioningUpdates \
	    -authenticationKeyPath "$(ASC_P8)" \
	    -authenticationKeyID "$(ASC_KEY)" \
	    -authenticationKeyIssuerID "$(ASC_ISSUER)" && \
	  xcrun altool --upload-app -f build/export/Splitty.ipa -t ios \
	    --apiKey "$(ASC_KEY)" --apiIssuer "$(ASC_ISSUER)"
