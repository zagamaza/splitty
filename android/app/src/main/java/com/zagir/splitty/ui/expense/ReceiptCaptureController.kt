package com.zagir.splitty.ui.expense

import android.Manifest
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import android.media.ExifInterface
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.FileProvider
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.InputStream
import kotlin.math.max
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Максимальная сторона фото чека — модели хватает 1024px, крупнее — лишний трафик. */
private const val RECEIPT_MAX_DIMENSION = 1024

/** Качество JPEG чека: 70 — читаемо для OCR/Gemini, но заметно легче исходника. */
private const val RECEIPT_JPEG_QUALITY = 70

/**
 * Съёмка/выбор фото чека для AI-распознавания (Task 7). Контроллер отдаёт две
 * команды — открыть Photo Picker или снять фото камерой; результат (готовый JPEG
 * в cacheDir) уходит в `onReceipt`, переданный в [rememberReceiptCapture].
 *
 * Обработка изображения — в чистых функциях ([encodeReceiptJpeg],
 * [decodeDownscaledReceipt]); их покрывают Robolectric-тесты без устройства.
 */
class ReceiptCaptureController internal constructor(
    private val hasCameraPermission: () -> Boolean,
    private val launchGallery: () -> Unit,
    private val launchCameraOrAskPermission: () -> Unit,
) {
    /** Открыть системный Photo Picker (по контракту PickVisualMedia разрешения не нужны). */
    fun pickFromGallery() = launchGallery()

    /**
     * Снять фото камерой. Нет разрешения CAMERA — сперва спрашиваем его, съёмка
     * стартует в коллбэке выдачи (по контракту разрешений Compose).
     */
    fun captureFromCamera() = launchCameraOrAskPermission()

    /** Есть ли уже разрешение на камеру (для показа rationale на экране). */
    val cameraPermissionGranted: Boolean get() = hasCameraPermission()
}

/**
 * Готовит контроллер съёмки чека. [onReceipt] получает путь к JPEG в cacheDir
 * (даунскейл + поворот по EXIF), когда пользователь выбрал/снял фото. Битый ввод
 * молча игнорируется (пользователь повторит). Обработка — в фоне (IO).
 *
 * [onCameraDenied] зовётся при отказе в разрешении CAMERA: экран показывает
 * подсказку, иначе тап по «Добавить фото чека» выглядел бы как зависание.
 * [onCameraUnavailable] — камеры на устройстве нет вовсе (см. [launchCamera]).
 */
@Composable
fun rememberReceiptCapture(
    onCameraDenied: () -> Unit = {},
    onCameraUnavailable: () -> Unit = onCameraDenied,
    onReceipt: (String) -> Unit,
): ReceiptCaptureController {
    val context = LocalContext.current
    // rememberUpdatedState: лаunchers создаются один раз, но зовут свежий onReceipt.
    val currentOnReceipt by rememberUpdatedState(onReceipt)
    val currentOnCameraDenied by rememberUpdatedState(onCameraDenied)
    val currentOnCameraUnavailable by rememberUpdatedState(onCameraUnavailable)
    val scope = rememberCoroutineScope()

    // Файл-цель камеры пересоздаётся на каждую съёмку; храним между launch и коллбэком.
    val cameraTarget = remember { arrayOfNulls<File>(1) }

    fun deliver(load: () -> ByteArray?) {
        scope.launch {
            val bytes = withContext(Dispatchers.IO) { runCatching { load() }.getOrNull() } ?: return@launch
            val path = withContext(Dispatchers.IO) { writeReceiptToCache(context, bytes) }
            currentOnReceipt(path)
        }
    }

    val galleryLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.PickVisualMedia()
    ) { uri ->
        if (uri != null) deliver { decodeDownscaledReceipt(context, uri) }
    }

    val cameraLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.TakePicture()
    ) { success ->
        // remember не переживает смерть процесса, а запуск камеры — один из самых
        // частых её триггеров: cameraTarget[0] возвращался null и снимок молча
        // выбрасывался. Путь детерминированный, поэтому просто пересчитываем.
        val file = cameraTarget[0] ?: newCameraOutputUri(context).second
        if (success) deliver { decodeDownscaledReceipt(context, Uri.fromFile(file)) }
    }

    fun launchCamera() {
        val (uri, file) = newCameraOutputUri(context)
        cameraTarget[0] = file
        try {
            cameraLauncher.launch(uri)
        } catch (_: ActivityNotFoundException) {
            // camera.any в манифесте объявлена required="false" — приложение
            // ставится и на устройства без камеры (эмулятор, ТВ-приставка,
            // урезанный OEM-образ). Там TakePicture не находит обработчика и
            // необработанное исключение убивало процесс на тапе «Снять чек».
            currentOnCameraUnavailable()
        }
    }

    val permissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted -> if (granted) launchCamera() else currentOnCameraDenied() }

    fun hasCameraPermission(): Boolean =
        context.checkSelfPermission(Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED

    return remember {
        ReceiptCaptureController(
            hasCameraPermission = ::hasCameraPermission,
            launchGallery = {
                galleryLauncher.launch(
                    PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)
                )
            },
            launchCameraOrAskPermission = {
                if (hasCameraPermission()) launchCamera()
                else permissionLauncher.launch(Manifest.permission.CAMERA)
            },
        )
    }
}

/**
 * Кодирует [bitmap] в JPEG чека: даунскейл так, чтобы бОльшая сторона была
 * ≤ [maxDimension], поворот на [rotationDegrees] (из EXIF), сжатие с [quality].
 * Чистая функция — тестируется на Robolectric.
 */
fun encodeReceiptJpeg(
    bitmap: Bitmap,
    rotationDegrees: Int = 0,
    maxDimension: Int = RECEIPT_MAX_DIMENSION,
    quality: Int = RECEIPT_JPEG_QUALITY,
): ByteArray {
    val scaled = scaleDown(bitmap, maxDimension)
    val oriented = if (rotationDegrees % 360 != 0) {
        val matrix = Matrix().apply { postRotate(rotationDegrees.toFloat()) }
        Bitmap.createBitmap(scaled, 0, 0, scaled.width, scaled.height, matrix, true)
    } else {
        scaled
    }
    return ByteArrayOutputStream().use { out ->
        oriented.compress(Bitmap.CompressFormat.JPEG, quality, out)
        out.toByteArray()
    }
}

/** Уменьшает bitmap так, чтобы бОльшая сторона была ≤ [maxDimension]; меньшие — как есть. */
private fun scaleDown(bitmap: Bitmap, maxDimension: Int): Bitmap {
    val longest = max(bitmap.width, bitmap.height)
    if (longest <= maxDimension) return bitmap
    val ratio = maxDimension.toFloat() / longest
    val width = (bitmap.width * ratio).toInt().coerceAtLeast(1)
    val height = (bitmap.height * ratio).toInt().coerceAtLeast(1)
    return Bitmap.createScaledBitmap(bitmap, width, height, true)
}

/**
 * Читает картинку из [uri] (галерея/камера) и готовит JPEG чека: сначала decode
 * bounds → inSampleSize (не тащим в память полноразмерное фото), затем поворот по
 * EXIF-ориентации. Возвращает null, если поток недоступен или картинка битая.
 */
fun decodeDownscaledReceipt(context: Context, uri: Uri): ByteArray? {
    val resolver = context.contentResolver

    // 1) габариты без загрузки пикселей → коэффициент прореживания
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) } ?: return null
    if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null

    val decodeOptions = BitmapFactory.Options().apply {
        inSampleSize = sampleSizeFor(bounds.outWidth, bounds.outHeight, RECEIPT_MAX_DIMENSION)
    }
    val bitmap = resolver.openInputStream(uri)?.use {
        BitmapFactory.decodeStream(it, null, decodeOptions)
    } ?: return null

    val rotation = resolver.openInputStream(uri)?.use { exifRotationDegrees(it) } ?: 0
    return encodeReceiptJpeg(bitmap, rotation)
}

/** Ближайшая степень двойки, при которой стороны укладываются в [maxDimension] (контракт inSampleSize). */
internal fun sampleSizeFor(width: Int, height: Int, maxDimension: Int): Int {
    var sample = 1
    var w = width
    var h = height
    // ИЛИ, не И: чек — узкая длинная лента (напр. 1200×9000). При «и» условие
    // рвалось на короткой стороне, subsample оставался 1 и в память улетал
    // полноразмерный битмап (~43 МБ на ARGB_8888) — OOM на бюджетных телефонах.
    // Уменьшать надо, пока в лимит не уложится ДЛИННАЯ сторона.
    while (w / 2 >= maxDimension || h / 2 >= maxDimension) {
        w /= 2
        h /= 2
        sample *= 2
    }
    return sample
}

/** Градусы поворота из EXIF-ориентации (камеры часто пишут повёрнутый сенсор). */
private fun exifRotationDegrees(input: InputStream): Int =
    when (ExifInterface(input).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL)) {
        ExifInterface.ORIENTATION_ROTATE_90 -> 90
        ExifInterface.ORIENTATION_ROTATE_180 -> 180
        ExifInterface.ORIENTATION_ROTATE_270 -> 270
        else -> 0
    }

/** Пишет готовый JPEG чека в cacheDir; путь переживает process death. */
fun writeReceiptToCache(context: Context, bytes: ByteArray): String {
    val dir = File(context.cacheDir, "receipts").apply { mkdirs() }
    // Имя фиксированное (детерминизм, без Random/времени): одновременно нужен
    // только один «текущий» кадр чека — перезаписываем.
    val file = File(dir, "receipt.jpg")
    file.writeBytes(bytes)
    return file.absolutePath
}

/** content-Uri во временный файл камеры (FileProvider без внешних прав хранилища). */
internal fun newCameraOutputUri(context: Context): Pair<Uri, File> {
    val dir = File(context.cacheDir, "receipts").apply { mkdirs() }
    val file = File(dir, "camera-capture.jpg")
    val uri = FileProvider.getUriForFile(context, "${context.packageName}.fileprovider", file)
    return uri to file
}
