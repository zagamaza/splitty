package com.zagir.splitty

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import com.zagir.splitty.core.session.PendingJoin
import com.zagir.splitty.core.session.PendingJoinStore
import com.zagir.splitty.ui.groups.parseRoomCode
import java.io.File
import java.nio.file.Files
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking

/**
 * Диплинк-вход в группу: разбор ссылки-приглашения ([parseRoomCode]) и
 * отложенное намерение вступить ([PendingJoinStore]).
 *
 * Парсер — единственный в приложении, и через него проходят ВСЕ четыре формата
 * (app link, кастомная схема, легаси-ссылка бота, голый код), поэтому его
 * поведение фиксируется построчно: ошибка здесь ломает и экран «Присоединиться»,
 * и вход по ссылке одновременно.
 */
class PendingJoinTest {

    /** Валидный код комнаты: mongo ObjectID, ровно 24 hex-символа. */
    private val code = "507f1f77bcf86cd799439011"

    // MARK: - parseRoomCode

    @Test
    fun `app link is parsed`() {
        assertEquals(code, parseRoomCode("https://splitty.app/join/$code"))
    }

    @Test
    fun `custom scheme link is parsed`() {
        assertEquals(code, parseRoomCode("splitty://join/$code"))
    }

    @Test
    fun `app link with trailing tail is parsed`() {
        // Трекеры и якоря дописывают «хвост» — код обязан обрезаться по нему.
        assertEquals(code, parseRoomCode("https://splitty.app/join/$code?utm_source=tg"))
        assertEquals(code, parseRoomCode("https://splitty.app/join/$code/"))
    }

    @Test
    fun `legacy bot link is parsed`() {
        assertEquals(code, parseRoomCode("https://t.me/split_money_bot?start=room$code"))
    }

    @Test
    fun `legacy room prefix is parsed`() {
        assertEquals(code, parseRoomCode("room$code"))
    }

    @Test
    fun `bare code is parsed`() {
        assertEquals(code, parseRoomCode(code))
    }

    @Test
    fun `surrounding whitespace is ignored`() {
        assertEquals(code, parseRoomCode("  $code\n"))
    }

    @Test
    fun `uppercase code is normalized to lower case`() {
        // Из буфера код может прийти в верхнем регистре; ObjectID из Go — всегда
        // в нижнем, и один и тот же код не должен выглядеть двумя разными.
        assertEquals(code, parseRoomCode(code.uppercase()))
    }

    @Test
    fun `foreign url is rejected`() {
        assertNull(parseRoomCode("https://example.com/some/page"))
        assertNull(parseRoomCode("https://splitty.app/about"))
    }

    @Test
    fun `garbage is rejected`() {
        assertNull(parseRoomCode(""))
        assertNull(parseRoomCode("   "))
        assertNull(parseRoomCode("посмотри эту группу"))
    }

    @Test
    fun `incomplete code is rejected`() {
        // Недобранный код гасит кнопку «Присоединиться» ДО запроса, а не даёт
        // 404 после него.
        assertNull(parseRoomCode(code.dropLast(1)))
        assertNull(parseRoomCode("https://splitty.app/join/abc"))
    }

    @Test
    fun `too long code is rejected`() {
        // 25-й hex-символ означает, что это не ObjectID, а что-то другое.
        assertNull(parseRoomCode(code + "a"))
    }

    @Test
    fun `non ascii digits are rejected`() {
        // Char.isDigit() считает цифрами и арабо-индийские — такой «код» уехал
        // бы на сервер мусором.
        assertNull(parseRoomCode("٥٠٧f1f77bcf86cd799439011"))
    }

    // MARK: - PendingJoinStore

    private lateinit var dir: File
    private lateinit var scope: CoroutineScope
    private lateinit var dataStore: DataStore<Preferences>

    @BeforeTest
    fun setUp() {
        dir = Files.createTempDirectory("pending-join-test").toFile()
        scope = CoroutineScope(Job() + Dispatchers.IO)
        dataStore = PreferenceDataStoreFactory.create(scope = scope) {
            File(dir, "session.preferences_pb")
        }
    }

    @AfterTest
    fun tearDown() {
        scope.cancel()
        dir.deleteRecursively()
    }

    @Test
    fun `take returns nothing when no intent stored`() = runBlocking {
        assertNull(PendingJoinStore(dataStore).take())
    }

    @Test
    fun `take returns intent and clears it`() = runBlocking {
        val store = PendingJoinStore(dataStore)
        store.set(code)

        assertEquals(code, store.take()?.roomId)
        // Повторный take пуст: вступление выполняется ровно один раз, иначе
        // второй запрос ушёл бы уже от имени следующего вошедшего.
        assertNull(store.take())
    }

    @Test
    fun `pending flow reflects set and take`() = runBlocking {
        val store = PendingJoinStore(dataStore)
        assertNull(store.pending.first())

        store.set(code)
        assertEquals(code, store.pending.first()?.roomId)

        store.take()
        assertNull(store.pending.first())
    }

    @Test
    fun `clear forgets intent`() = runBlocking {
        val store = PendingJoinStore(dataStore)
        store.set(code)

        store.clear()

        assertNull(store.take())
    }

    @Test
    fun `intent survives store recreation`() = runBlocking {
        // Вход через Google уводит в системный лист и в другое приложение —
        // процесс могут выгрузить. Намерение обязано пережить это.
        PendingJoinStore(dataStore).set(code)

        assertEquals(code, PendingJoinStore(dataStore).take()?.roomId)
    }

    // MARK: - владелец намерения

    @Test
    fun `owner is stored with the intent and survives recreation`() = runBlocking {
        // Владельца обязательно писать на ДИСК: между протуханием сессии и
        // следующим входом процесс успевает умереть (вход уводит в системный
        // лист), и признак в памяти к этому моменту уже потерян.
        PendingJoinStore(dataStore).set(code, ownerId = 42)

        assertEquals(42L, PendingJoinStore(dataStore).pending.first()?.ownerId)
    }

    @Test
    fun `guest intent is adopted by the first signed in user`() = runBlocking {
        // Штатный путь приглашения: ссылку открыл гость, вступление доезжает
        // сразу после входа — намерение становится его.
        val store = PendingJoinStore(dataStore)
        store.set(code, ownerId = null)

        store.reconcileOwner(7)

        assertEquals(PendingJoin(code, 7), store.pending.first())
    }

    @Test
    fun `own intent survives reconcile`() = runBlocking {
        val store = PendingJoinStore(dataStore)
        store.set(code, ownerId = 7)

        store.reconcileOwner(7)

        assertEquals(PendingJoin(code, 7), store.pending.first())
    }

    @Test
    fun `intent of another account is dropped`() = runBlocking {
        // Главный сценарий утечки: сессия A протухла (её чистка приглашение
        // намеренно сохраняет), а вошёл на устройстве уже B. Без этой проверки
        // B молча вступал бы в приватную группу A.
        val store = PendingJoinStore(dataStore)
        store.set(code, ownerId = 1)

        store.reconcileOwner(2)

        assertNull(store.pending.first())
    }

    @Test
    fun `reconcile does nothing when there is no intent`() = runBlocking {
        val store = PendingJoinStore(dataStore)

        store.reconcileOwner(5)

        assertNull(store.pending.first())
    }

    @Test
    fun `clear forgets the owner too`() = runBlocking {
        // Иначе следующее намерение того же устройства унаследовало бы
        // владельца от предыдущего.
        val store = PendingJoinStore(dataStore)
        store.set(code, ownerId = 1)
        store.clear()

        store.set(code)

        assertNull(store.pending.first()?.ownerId)
    }
}
