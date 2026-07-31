package com.zagir.splitty.core.auth

import android.content.Context
import android.util.Log
import androidx.credentials.CredentialManager
import androidx.credentials.CustomCredential
import androidx.credentials.GetCredentialRequest
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.credentials.exceptions.GetCredentialException
import androidx.credentials.exceptions.NoCredentialException
import com.google.android.libraries.identity.googleid.GetGoogleIdOption
import com.google.android.libraries.identity.googleid.GoogleIdTokenCredential
import com.google.android.libraries.identity.googleid.GoogleIdTokenParsingException
import com.zagir.splitty.BuildConfig
import javax.inject.Inject
import javax.inject.Singleton

/** Ошибка получения id-токена, пригодная для показа пользователю. */
class GoogleSignInException(message: String, cause: Throwable? = null) : Exception(message, cause)

/**
 * Единственное место, где живёт Credential Manager (порт iOS
 * `GoogleSignInService`): наружу отдаётся голый id-токен, а SDK не растекается
 * по ViewModel и экрану. Интерфейс — ради юнит-тестов входа: реальный
 * Credential Manager требует Play Services и системный UI, в JVM его нет.
 */
interface GoogleIdTokenProvider {
    /**
     * Показывает системный выбор Google-аккаунта.
     *
     * @param activityContext контекст АКТИВИТИ — Credential Manager рисует свой
     *   лист поверх неё; с application-контекстом вызов падает.
     * @return id-токен, либо `null`, если пользователь закрыл лист (отмена —
     *   не ошибка и алерта не заслуживает).
     * @throws GoogleSignInException при любой другой неудаче.
     */
    suspend fun idToken(activityContext: Context): String?
}

private const val TAG = "GoogleIdToken"

@Singleton
class CredentialManagerGoogleIdTokenProvider @Inject constructor() : GoogleIdTokenProvider {

    override suspend fun idToken(activityContext: Context): String? {
        val option = GetGoogleIdOption.Builder()
            // Серверный (Web) client id: он же `aud` выданного токена, его
            // сверяет бэкенд. Android-клиенты Google подставляет сам по
            // package name + подписи, в код они не попадают.
            .setServerClientId(BuildConfig.GOOGLE_SERVER_CLIENT_ID)
            // false — показываем ВСЕ аккаунты устройства, а не только уже
            // авторизованные в приложении: при первом входе авторизованных нет
            // вовсе, и с true лист был бы пустым (NoCredentialException).
            .setFilterByAuthorizedAccounts(false)
            // Автовыбор единственного аккаунта выключен: вход должен быть
            // осознанным действием, а не побочным эффектом тапа.
            .setAutoSelectEnabled(false)
            .build()
        val request = GetCredentialRequest.Builder().addCredentialOption(option).build()

        val response = try {
            CredentialManager.create(activityContext).getCredential(activityContext, request)
        } catch (e: GetCredentialCancellationException) {
            return null
        } catch (e: NoCredentialException) {
            // На устройстве нет ни одного Google-аккаунта — сообщение про
            // «ошибку входа» тут бесполезно, человеку нужно действие.
            throw GoogleSignInException("Добавьте Google-аккаунт в настройках устройства", e)
        } catch (e: GetCredentialException) {
            Log.e(TAG, "credential manager failed", e)
            throw GoogleSignInException("Не удалось войти через Google", e)
        }

        val credential = response.credential
        if (credential is CustomCredential &&
            credential.type == GoogleIdTokenCredential.TYPE_GOOGLE_ID_TOKEN_CREDENTIAL
        ) {
            return try {
                GoogleIdTokenCredential.createFrom(credential.data).idToken
            } catch (e: GoogleIdTokenParsingException) {
                Log.e(TAG, "google id token parsing failed", e)
                throw GoogleSignInException("Не удалось войти через Google", e)
            }
        }
        // Другой тип учётки (например, пароль из менеджера) для /auth/google
        // бесполезен: обменять на сессию нечего.
        Log.e(TAG, "unexpected credential type: ${credential.type}")
        throw GoogleSignInException("Не удалось войти через Google")
    }
}
