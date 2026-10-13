# Гонка DataStore: потеря обновления у нового подписчика

Найдено Codex 13.10.2026 при разборе нестабильного `AppRootJoinTest`
(приглашение не переходило к вошедшему аккаунту, ожидание по 30 с).

- В `gradle/libs.versions.toml` объявлен DataStore 1.1.1, но транзитивно
  разрешается **1.1.7** (`./gradlew :app:dependencyInsight --dependency datastore`).
- В 1.1.7 есть ошибка Google [b/431787506](https://issuetracker.google.com/issues/431787506):
  подписчик `data`, начавший чтение во время записи, может остаться на старом
  значении.
- Исправление (I6a427) впервые в [1.3.0-alpha03](https://developer.android.com/jetpack/androidx/releases/datastore#1.3.0-alpha03).
  Стабильная 1.2.1 его **не** содержит.

Воспроизведение без приложения и без `Dispatchers.Main` перенесено в регрессионные
тесты: `app/src/test/java/com/zagir/splitty/diagnostics/DataStoreReadDuringWriteRaceTest.kt`.

| Версия DataStore | Результат |
| --- | --- |
| 1.1.7 (сейчас) | красный: подписчик видит `[1]`, свежий `first()` — `2` |
| 1.3.0-rc01 | зелёный |

13.10.2026 DataStore обновлён до 1.3.0-rc01 (релиз-кандидат) — см. комментарий
в `gradle/libs.versions.toml`. Когда выйдет стабильная 1.3.x — перейти на неё.
