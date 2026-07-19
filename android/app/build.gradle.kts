import java.util.Properties
import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ksp)
    alias(libs.plugins.hilt)
    alias(libs.plugins.roborazzi)
    alias(libs.plugins.firebase.appdistribution)
}

android {
    namespace = "com.zagir.splitty"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.zagir.splitty"
        minSdk = 26
        targetSdk = 36
        versionCode = 5
        versionName = "1.3"

        // Караоке-транскрипт в оверлее записи (Task 13) — «лестница»: платформенный
        // SpeechRecognizer (API 33+) → Vosk-модель on-demand → без караоке. Пока
        // выключен: качество распознавания на устройствах проверяется PoC-прогоном,
        // а сам parse работает от аудио и без транскрипта.
        buildConfigField("boolean", "KARAOKE_TRANSCRIPT", "false")
    }

    // Подпись release: ключ и пароли в keystore.properties (в .gitignore).
    val keystoreProps = Properties().apply {
        val f = rootProject.file("keystore.properties")
        if (f.exists()) f.inputStream().use { load(it) }
    }
    // Firebase App Distribution: appId, группа тестеров и путь к service-account
    // json. Файла нет (локальная сборка/CI без раздачи) — берутся дефолты, а
    // задача appDistributionUpload просто не запускается.
    val firebaseProps = Properties().apply {
        val f = rootProject.file("firebase.properties")
        if (f.exists()) f.inputStream().use { load(it) }
    }

    signingConfigs {
        if (keystoreProps.isNotEmpty()) {
            create("release") {
                storeFile = rootProject.file(keystoreProps.getProperty("storeFile"))
                storePassword = keystoreProps.getProperty("storePassword")
                keyAlias = keystoreProps.getProperty("keyAlias")
                keyPassword = keystoreProps.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            // R8 + удаление неиспользуемых ресурсов. Всё, что резолвится
            // рефлексией (Retrofit-интерфейсы, сериализаторы, Vosk), держится
            // явными правилами в proguard-rules.pro — иначе падает в рантайме,
            // а не на сборке.
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            if (keystoreProps.isNotEmpty()) {
                signingConfig = signingConfigs.getByName("release")
            }

            // Раздача тестерам: ./gradlew :app:assembleRelease
            // appDistributionUploadRelease. Группа и креды — из
            // firebase.properties/переменных окружения (см. android/README).
            firebaseAppDistribution {
                artifactType = "APK"
                groups = firebaseProps.getProperty("groups", "testers")
                firebaseProps.getProperty("serviceCredentialsFile")?.let {
                    serviceCredentialsFile = it
                }
                firebaseProps.getProperty("appId")?.let { appId = it }
                releaseNotesFile = "app/release-notes.txt"
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    testOptions {
        unitTests {
            // Roborazzi/Robolectric рендерят Compose с реальными ресурсами темы.
            isIncludeAndroidResources = true
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)

    val composeBom = platform(libs.compose.bom)
    implementation(composeBom)
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.graphics)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons.extended)
    debugImplementation(libs.compose.ui.tooling)

    implementation(libs.navigation.compose)

    implementation(libs.hilt.android)
    ksp(libs.hilt.compiler)
    implementation(libs.hilt.navigation.compose)

    implementation(libs.retrofit)
    implementation(libs.retrofit.kotlinx.serialization)
    implementation(libs.okhttp)
    implementation(libs.okhttp.logging)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)

    implementation(libs.datastore.preferences)

    testImplementation(libs.junit)
    testImplementation(libs.kotlin.test)
    testImplementation(libs.kotlinx.coroutines.test)
    // MockWebServer — multipart parse-запроса и коды ошибок (413/415/429/503).
    testImplementation(libs.okhttp.mockwebserver)

    // Скриншот-тесты дизайн-системы (Roborazzi поверх Robolectric).
    testImplementation(libs.robolectric)
    testImplementation(libs.roborazzi)
    testImplementation(libs.roborazzi.compose)
    testImplementation(libs.roborazzi.junit.rule)
    testImplementation(composeBom)
    testImplementation(libs.compose.ui.test.junit4)
    // Манифест с ComponentActivity нужен в debug-варианте (под ним крутится
    // Robolectric) — иначе ActivityScenario не резолвит активити для Compose.
    debugImplementation(libs.compose.ui.test.manifest)
}

/**
 * Smoke минифицированной сборки: R8 «успешно» собирает APK и тогда, когда
 * выкинул сериализаторы или Retrofit-интерфейсы — падает это уже в рантайме у
 * тестера. Задача читает mapping.txt и требует, чтобы всё рефлексивное дожило
 * до APK. Запускается сама после assembleRelease.
 */
val verifyReleaseShrinking by tasks.registering {
    description = "Проверяет, что R8 не выкинул сериализаторы и Retrofit-интерфейсы"
    val mapping = layout.buildDirectory.file("outputs/mapping/release/mapping.txt")
    inputs.file(mapping)
    doLast {
        val text = mapping.get().asFile.readText()
        val survivors = Regex("""^(com\.zagir\.splitty\.[\w.$]+) ->""", RegexOption.MULTILINE)
            .findAll(text).map { it.groupValues[1] }.toSet()

        val serializers = survivors.count { it.endsWith("\$\$serializer") }
        require(serializers >= 30) {
            "R8 выкинул сериализаторы моделей: в mapping их $serializers (ожидали ≥30). " +
                "Проверь -keep для kotlinx.serialization в proguard-rules.pro."
        }
        listOf(
            "com.zagir.splitty.core.network.SplittyApi",
            "com.zagir.splitty.core.network.ParseApi",
            "com.zagir.splitty.MainActivity",
            "com.zagir.splitty.SplittyApp",
        ).forEach { required ->
            require(required in survivors) { "R8 выкинул $required — сборка нерабочая." }
        }
        logger.lifecycle("R8-smoke: сериализаторов $serializers, интерфейсы API и точки входа на месте.")
    }
}

tasks.matching { it.name == "assembleRelease" }.configureEach {
    finalizedBy(verifyReleaseShrinking)
}

// Robolectric 4.13 не поддерживает JDK 24 (major 68) — юнит-тесты гоняем на
// установленном JDK 21. Основную сборку/компиляцию это не трогает.
tasks.withType<Test>().configureEach {
    javaLauncher.set(
        javaToolchains.launcherFor {
            languageVersion.set(JavaLanguageVersion.of(21))
        }
    )
}
