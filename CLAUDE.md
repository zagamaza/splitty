# Splitty / Splitor

В сторе приложение называется **Splitor**, в репозитории и bundle id — `splitty`.
Источник правды для витрин обоих магазинов — каталог `metadata/`, а не веб-интерфейсы
(см. `metadata/README.md`). Правка в консоли переживёт ровно до следующей заливки.

## Релиз iOS: всё делается по API, браузер не нужен

Отправка в ревью не требует ни входа в App Store Connect, ни двухфакторного кода.
Ключ уже есть, и им закрывается весь путь — смена версии, привязка сборки, submit.

```
ключ    ~/.appstoreconnect/private_keys/AuthKey_T6PMYHX4T7.p8
key id  T6PMYHX4T7
issuer  a30d44ef-0dc4-4c01-bb7b-7235968f61f8
app id  6787746052   (com.zagir.splitty)
team    K8922Y6R3M
```

Те же значения лежат в `Makefile` (`ASC_KEY`, `ASC_ISSUER`, `ASC_P8`).

**Порядок:**

1. Поднять версию в `ios/project.yml` (`CFBundleShortVersionString`, `CFBundleVersion`),
   затем `xcodegen generate`.
2. `make ios-publish` — архив, экспорт, загрузка через `altool`. Ждать
   `UPLOAD SUCCEEDED`; сборка появляется в API за пару минут и должна дойти до
   `processingState: VALID`.
3. Дальше — App Store Connect API (JWT ES256, `aud: appstoreconnect-v1`, `kid` = key id):
   - `GET /v1/apps/{appId}/appStoreVersions` — найти запись версии;
   - `PATCH /v1/appStoreVersions/{id}` — **номер версии обязан совпадать с
     `CFBundleShortVersionString` сборки**, иначе сборку к ней не привязать;
   - `PATCH /v1/appStoreVersions/{id}/relationships/build` — привязать сборку;
   - `GET /v1/reviewSubmissions?filter[app]={appId}` — после отказа заявка остаётся
     жить в состоянии `UNRESOLVED_ISSUES`; **новую создавать не нужно**, отправляется
     повторно та же;
   - `PATCH /v1/reviewSubmissions/{id}` с `attributes.submitted = true`.

**Грабли:** сразу после привязки сборки submit отвечает `409 STATE_ERROR` —
«Version is not ready to be submitted yet, please try again later». Это временно,
Apple дописывает валидацию; помогает повтор через минуту-другую.

**Чего в API нет:** текста отказа ревьюера. Resolution Center наружу не отдаётся —
за причиной отказа придётся идти в веб-интерфейс. Инструменты `aso-mcp` (`connect_*`)
тоже не помогут: они только про метаданные витрины.
