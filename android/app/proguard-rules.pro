# Правила R8 для release (isMinifyEnabled = true, shrinkResources = true).
# Держим только то, что резолвится рефлексией: без этих правил сборка проходит,
# а падает уже в рантайме у тестера — поэтому каждое правило с объяснением.

# --- kotlinx.serialization -------------------------------------------------
# Сериализаторы ищутся через companion/статический serializer(): имена
# переименовывать нельзя, аннотации нужны для метаданных.
-keepattributes *Annotation*, InnerClasses, Signature, RuntimeVisibleAnnotations
-dontnote kotlinx.serialization.**
-keepclassmembers class com.zagir.splitty.** {
    *** Companion;
}
-keepclasseswithmembers class com.zagir.splitty.** {
    kotlinx.serialization.KSerializer serializer(...);
}
# @Serializable-классы: R8 не должен выкидывать синтетические $$serializer.
-if @kotlinx.serialization.Serializable class **
-keepclassmembers class <1> {
    static <1>$Companion Companion;
    static **$* *;
}
-keepclassmembers class **$$serializer {
    *** INSTANCE;
    *** descriptor;
}
# Константы @Serializable-энумов. R8 в full mode выбрасывает те, на которые нет
# статических ссылок в коде: у OutboxKind в release оставался только CREATE, и
# outbox.json с "kind":"update"/"delete" (запись предыдущей версии приложения)
# ронял разбор всего списка — вся офлайн-очередь терялась. Правило выше про
# $$serializer это не покрывает: у энумов его не генерируется.
-keepclassmembers @kotlinx.serialization.Serializable enum com.zagir.splitty.** {
    <fields>;
    **[] values();
    ** valueOf(java.lang.String);
}

# --- Retrofit / OkHttp -----------------------------------------------------
# Интерфейсы API — динамические прокси: и сам интерфейс, и его дженерик-
# сигнатуры (тип ответа читается рефлексией) должны пережить R8.
-keep,allowobfuscation,allowshrinking interface com.zagir.splitty.core.network.SplittyApi
-keep,allowobfuscation,allowshrinking interface com.zagir.splitty.core.network.ParseApi
-keepattributes Exceptions
-keep,allowobfuscation,allowshrinking interface retrofit2.Call
-keep,allowobfuscation,allowshrinking class retrofit2.Response
-keep,allowobfuscation,allowshrinking class kotlin.coroutines.Continuation
# Аннотации методов Retrofit (@POST/@Multipart/@Part) читаются рефлексией.
-keepclassmembers,allowshrinking,allowobfuscation interface * {
    @retrofit2.http.* <methods>;
}
# OkHttp/Okio тянут опциональные классы (Conscrypt, BouncyCastle, Animal Sniffer),
# которых в APK нет — это предупреждения, а не ошибки.
-dontwarn okhttp3.internal.platform.**
-dontwarn org.conscrypt.**
-dontwarn org.bouncycastle.**
-dontwarn org.openjsse.**
-dontwarn org.codehaus.mojo.animal_sniffer.**

# --- Vosk (караоке-транскрипт за флагом KARAOKE_TRANSCRIPT) ----------------
# Библиотека приезжает докачкой и вызывается через рефлексивный мост, поэтому
# её классы для R8 «неиспользуемые». Правила не ломают сборку без Vosk.
-keep class org.vosk.** { *; }
-keep class com.sun.jna.** { *; }
-dontwarn org.vosk.**
-dontwarn com.sun.jna.**

# --- Прочее ----------------------------------------------------------------
# Понятные стек-трейсы в отчётах тестеров (маппинг остаётся в build/outputs).
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile
