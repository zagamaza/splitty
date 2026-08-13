#!/bin/sh
# Выкатка бэкенда Splitty из GitHub Actions.
#
# Это ФОРСИРОВАННАЯ команда deploy-ключа (command= в authorized_keys): чем бы
# ни представился клиент, выполняется только этот скрипт, а запрошенное им
# лежит в SSH_ORIGINAL_COMMAND. Отсюда же и разбор аргументов вручную —
# принимаем ровно два случая:
#
#   load               образ приезжает потоком на stdin (docker save | ssh ... load)
#   deploy sha-1234567 переключить прод на уже загруженный образ
set -eu

APP_DIR=/home/splitit/app
COMPOSE="$APP_DIR/docker-compose.yaml"
SERVICE=telegram-bot-prod
HEALTH_URL=http://127.0.0.1:18002/health
HEALTH_TRIES=30
HEALTH_DELAY=2
KEEP_IMAGES=5
LOCK=/var/lock/splitty-deploy.lock

cmd="${SSH_ORIGINAL_COMMAND:-}"

log() { echo "[deploy] $*"; }

health_ok() {
    body=$(curl -fsS -m 5 "$HEALTH_URL" 2>/dev/null) || return 1
    echo "$body" | grep -q '"status":"ok"' || return 1
    # db отдельно: контейнер может подняться и отвечать, потеряв базу, —
    # такую «живую» выкатку принимать нельзя
    echo "$body" | grep -q '"db":"ok"' || return 1
    return 0
}

wait_healthy() {
    i=0
    while [ "$i" -lt "$HEALTH_TRIES" ]; do
        if health_ok; then return 0; fi
        i=$((i + 1))
        sleep "$HEALTH_DELAY"
    done
    return 1
}

case "$cmd" in
load)
    docker load
    ;;

"deploy "*)
    tag=${cmd#deploy }
    # Тег уходит в sed и в docker: всё, что не наш формат, отбиваем сразу
    echo "$tag" | grep -Eq '^sha-[0-9a-f]{7,40}$' || {
        log "плохой тег: $tag"
        exit 2
    }
    docker image inspect "splitty:$tag" >/dev/null 2>&1 || {
        log "образа splitty:$tag на сервере нет — сначала load"
        exit 3
    }

    # Две выкатки разом переписали бы compose друг поверх друга
    exec 9>"$LOCK"
    flock -w 300 9 || { log "другая выкатка не отпустила блокировку"; exit 4; }

    prev=$(sed -nE 's/^[[:space:]]*image: (splitty:.*)$/\1/p' "$COMPOSE" | head -1)
    [ -n "$prev" ] || { log "в compose нет активной строки image: splitty:*"; exit 5; }
    if [ "$prev" = "splitty:$tag" ]; then
        log "на проде уже $prev — перезапускаю на всякий случай"
    fi

    backup="$COMPOSE.bak-$(date +%Y%m%d-%H%M%S)"
    cp "$COMPOSE" "$backup"
    log "было $prev, ставлю splitty:$tag (бэкап $backup)"

    sed -i -E "s|^([[:space:]]*)image: splitty:.*|\1image: splitty:$tag|" "$COMPOSE"

    cd "$APP_DIR"
    if ! docker-compose up -d "$SERVICE"; then
        log "up -d не отработал, откатываюсь на $prev"
        cp "$backup" "$COMPOSE"
        docker-compose up -d "$SERVICE" || true
        exit 6
    fi

    if wait_healthy; then
        log "health ok, прод на splitty:$tag"
    else
        log "health не поднялся за $((HEALTH_TRIES * HEALTH_DELAY))с — откат на $prev"
        docker logs --tail 40 "$(docker-compose ps -q $SERVICE)" 2>&1 || true
        cp "$backup" "$COMPOSE"
        docker-compose up -d "$SERVICE" || true
        if wait_healthy; then
            log "откат удался, прод снова на $prev"
        else
            log "ВНИМАНИЕ: и откат не поднялся, нужен человек"
        fi
        exit 7
    fi

    # Старые образы копятся по 40 МБ на выкатку; текущий и свежие держим
    docker images --filter reference='splitty:*' --format '{{.CreatedAt}}\t{{.Repository}}:{{.Tag}}' |
        sort -r | tail -n +$((KEEP_IMAGES + 1)) | cut -f2 |
        grep -v "^splitty:$tag$" |
        while read -r old; do
            docker rmi "$old" >/dev/null 2>&1 && log "убрал старый образ $old" || true
        done

    # Бэкапы compose — туда же: держим последние 20
    ls -1t "$COMPOSE".bak-* 2>/dev/null | tail -n +21 | while read -r b; do rm -f "$b"; done
    ;;

*)
    log "этим ключом можно только 'load' и 'deploy sha-<хеш>', получено: '$cmd'"
    exit 1
    ;;
esac
