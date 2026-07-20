package com.zagir.splitty.core.network

import com.zagir.splitty.core.session.SessionStore
import javax.inject.Inject
import javax.inject.Singleton
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.Interceptor
import okhttp3.Response

/**
 * Подставляет ТЕКУЩИЙ адрес сервера из [SessionStore] вместо плейсхолдера
 * Retrofit: base URL меняется в рантайме (поле «Сервер» на экране входа).
 * Path-префикс базового адреса сохраняется («http://host/splitty» + «api/v1/…»
 * = «/splitty/api/v1/…» — сервер может жить за реверс-прокси).
 */
@Singleton
class BaseUrlInterceptor @Inject constructor(
    private val session: SessionStore,
) : Interceptor {

    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val base = session.currentBaseUrl().trim().trimEnd('/')
        val baseUrl = base.toHttpUrlOrNull() ?: throw InvalidBaseUrlException()
        val url = baseUrl.newBuilder()
            .addEncodedPathSegments(request.url.encodedPath.trimStart('/'))
            .encodedQuery(request.url.encodedQuery)
            .build()
        return chain.proceed(request.newBuilder().url(url).build())
    }
}

/**
 * Добавляет `Authorization: Bearer <JWT>` ко всем запросам, кроме auth-путей,
 * и делает ГЛОБАЛЬНЫЙ разлогин на любой 401 аутентифицированного запроса
 * (мёртвая сессия → экран входа; аналог iOS APIClient.onUnauthorized).
 * 401 на самих auth-путях (неверный код) разлогин НЕ вызывает — токена там нет.
 */
@Singleton
class AuthInterceptor @Inject constructor(
    private val session: SessionStore,
) : Interceptor {

    override fun intercept(chain: Interceptor.Chain): Response {
        var request = chain.request()
        val isAuthEndpoint = request.url.encodedPath.contains("/auth/")
        val token = session.currentToken()
        if (token != null && !isAuthEndpoint) {
            request = request.newBuilder()
                .header("Authorization", "Bearer $token")
                .build()
        }
        val response = chain.proceed(request)
        // Разлогиниваем, только если 401 пришёл на АКТУАЛЬНЫЙ токен. Проверка
        // «был ли заголовок» ловила и запрос, улетевший до перелогина: его
        // запоздалый 401 сносил уже новую сессию — вместе с ключом Keystore
        // и неотправленной офлайн-очередью (её logout чистит безвозвратно).
        if (response.code == 401 && token != null && session.currentToken() == token) {
            session.notifyUnauthorized()
        }
        return response
    }
}
