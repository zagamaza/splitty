package com.zagir.splitty.core.session

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Шифрование токена сессии. Вынесено за интерфейс, потому что реальный
 * [KeystoreTokenCipher] требует Android Keystore и в JVM-тестах недоступен —
 * тесты подставляют fake-реализацию (см. FakeTokenCipher в тестах).
 */
interface TokenCipher {
    /** Шифрует токен в строку `Base64(IV | ciphertext)`. */
    fun encrypt(plainText: String): String

    /**
     * Расшифровывает строку, полученную из [encrypt]. Возвращает null, если
     * ключ пропал (сброс Keystore, перенос на другое устройство) или данные
     * повреждены — вызывающий трактует это как отсутствие токена (разлогин).
     */
    fun decrypt(cipherText: String): String?

    /** Удаляет ключ из хранилища (logout): существующий шифротекст становится нечитаемым. */
    fun clearKey()
}

/**
 * AES-256-GCM поверх Android Keystore. Ключ не покидает Keystore; в DataStore
 * лежит только шифротекст (`Base64(IV | ciphertext+tag)`). EncryptedSharedPreferences
 * помечен deprecated, поэтому шифруем сами.
 */
class KeystoreTokenCipher : TokenCipher {

    private companion object {
        const val KEYSTORE = "AndroidKeyStore"
        const val KEY_ALIAS = "splitty_token_key"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val IV_LENGTH = 12
        const val TAG_BITS = 128
    }

    override fun encrypt(plainText: String): String {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, getOrCreateKey())
        val iv = cipher.iv // GCM: 12 байт, генерируется провайдером
        val bytes = cipher.doFinal(plainText.toByteArray(Charsets.UTF_8))
        return Base64.encodeToString(iv + bytes, Base64.NO_WRAP)
    }

    override fun decrypt(cipherText: String): String? = runCatching {
        val key = existingKey() ?: return null
        val raw = Base64.decode(cipherText, Base64.NO_WRAP)
        if (raw.size <= IV_LENGTH) return null
        val iv = raw.copyOfRange(0, IV_LENGTH)
        val body = raw.copyOfRange(IV_LENGTH, raw.size)
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(TAG_BITS, iv))
        String(cipher.doFinal(body), Charsets.UTF_8)
    }.getOrNull()

    override fun clearKey() {
        runCatching {
            keyStore().takeIf { it.containsAlias(KEY_ALIAS) }?.deleteEntry(KEY_ALIAS)
        }
    }

    private fun keyStore(): KeyStore = KeyStore.getInstance(KEYSTORE).apply { load(null) }

    private fun existingKey(): SecretKey? =
        (keyStore().getEntry(KEY_ALIAS, null) as? KeyStore.SecretKeyEntry)?.secretKey

    private fun getOrCreateKey(): SecretKey {
        existingKey()?.let { return it }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }
}
