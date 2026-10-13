package com.zagir.splitty.diagnostics

import androidx.datastore.core.DataStoreFactory
import androidx.datastore.core.Serializer
import java.io.File
import java.io.InputStream
import java.io.OutputStream
import java.nio.file.Files
import kotlin.test.assertEquals
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.take
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withTimeout
import org.junit.Test

/**
 * Гонка DataStore b/431787506: новый подписчик `data`, начавший чтение во
 * время записи, остаётся на старом значении, хотя свежий `first()` уже видит
 * новое. Запись 2 приостановлена сериализатором, пока подписчик не начнёт.
 *
 * Регрессионный: 1.1.7 — красный (подписчик видит только [1]), с 1.3.0-rc01 —
 * зелёный. Покраснел после смены версии — гонка вернулась.
 */
class DataStoreReadDuringWriteRaceTest {
    @Test
    fun readerStartedDuringWriteSeesTheWrite() = runTest {
        val dir = Files.createTempDirectory("ds-race").toFile()
        val job = Job()
        val writeStarted = CompletableDeferred<Unit>()
        val releaseWrite = CompletableDeferred<Unit>()
        val serializer = object : Serializer<Int> {
            override val defaultValue = 0
            override suspend fun readFrom(input: InputStream) = input.read().coerceAtLeast(0)
            override suspend fun writeTo(t: Int, output: OutputStream) {
                if (t == 2) {
                    writeStarted.complete(Unit)
                    releaseWrite.await()
                }
                output.write(t)
            }
        }
        val store = DataStoreFactory.create(serializer = serializer, scope = CoroutineScope(coroutineContext + job)) {
            File(dir, "value")
        }
        try {
            store.updateData { 1 }
            assertEquals(1, store.data.first())
            val writer = launch { store.updateData { 2 } }
            writeStarted.await()
            val seen = mutableListOf<Int>()
            val reader = launch { store.data.take(2).toList(seen) }
            releaseWrite.complete(Unit)
            writer.join()
            withTimeout(1_000) { reader.join() }
            assertEquals(listOf(1, 2), seen)
        } finally {
            job.cancelAndJoin()
            dir.deleteRecursively()
        }
    }
}
