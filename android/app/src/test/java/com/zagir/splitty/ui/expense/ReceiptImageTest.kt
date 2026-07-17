package com.zagir.splitty.ui.expense

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * Обработка фото чека (Task 7): даунскейл до 1024px + JPEG q0.7, и расчёт
 * inSampleSize. Bitmap-операции требуют Robolectric (NATIVE-графика).
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34])
class ReceiptImageTest {

    @Test
    fun `encodeReceiptJpeg downscales large bitmap to max dimension`() {
        val big = Bitmap.createBitmap(3000, 2000, Bitmap.Config.ARGB_8888)

        val jpeg = encodeReceiptJpeg(big)

        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(jpeg, 0, jpeg.size, bounds)
        assertEquals(1024, bounds.outWidth)
        assertTrue(bounds.outHeight in 682..683) // 2000 * 1024/3000 ≈ 682
    }

    @Test
    fun `encodeReceiptJpeg keeps small bitmap dimensions`() {
        val small = Bitmap.createBitmap(400, 300, Bitmap.Config.ARGB_8888)

        val jpeg = encodeReceiptJpeg(small)

        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(jpeg, 0, jpeg.size, bounds)
        assertEquals(400, bounds.outWidth)
        assertEquals(300, bounds.outHeight)
    }

    @Test
    fun `encodeReceiptJpeg rotates by 90 degrees swapping sides`() {
        val portrait = Bitmap.createBitmap(300, 500, Bitmap.Config.ARGB_8888)

        val jpeg = encodeReceiptJpeg(portrait, rotationDegrees = 90)

        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(jpeg, 0, jpeg.size, bounds)
        assertEquals(500, bounds.outWidth)
        assertEquals(300, bounds.outHeight)
    }

    @Test
    fun `sampleSizeFor picks power of two that fits max dimension`() {
        assertEquals(1, sampleSizeFor(1000, 800, 1024))
        assertEquals(2, sampleSizeFor(4000, 3000, 1024))
        assertEquals(4, sampleSizeFor(8000, 6000, 1024))
    }
}
