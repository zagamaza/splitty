package com.zagir.splitty.core.network

import com.zagir.splitty.core.model.ParseResponse
import okhttp3.MultipartBody
import retrofit2.http.Body
import retrofit2.http.POST
import retrofit2.http.Path

/**
 * Отдельный Retrofit-интерфейс AI-распознавания расхода. Живёт на СВОЁМ
 * OkHttp-клиенте с read/write timeout 90с (см. NetworkModule): singleton-клиент
 * имеет readTimeout 30с, а Gemini на фото/аудио отвечает дольше. Тело multipart
 * собирается вручную в репозитории (части draft/text/audio/image — имена и MIME
 * как в iOS APIClient), поэтому здесь — сырой [MultipartBody].
 */
interface ParseApi {

    @POST("api/v1/rooms/{roomId}/operations/parse")
    suspend fun parse(
        @Path("roomId") roomId: String,
        @Body body: MultipartBody,
    ): ParseResponse
}
