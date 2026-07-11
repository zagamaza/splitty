# Минификация выключена (isMinifyEnabled = false); правила — задел на будущее.
# kotlinx.serialization: сериализаторы ищутся рефлексией по companion.
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.**
-keepclassmembers class com.zagir.splitty.** {
    *** Companion;
}
-keepclasseswithmembers class com.zagir.splitty.** {
    kotlinx.serialization.KSerializer serializer(...);
}
