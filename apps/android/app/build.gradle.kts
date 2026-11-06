import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose.compiler)
    alias(libs.plugins.kotlin.serialization)
}

// Optional dev defaults in apps/android/spike.properties (git-ignored). They only prefill the
// login screen of the debug build; release builds get empty defaults.
val devProps = Properties().apply {
    val f = rootProject.file("spike.properties")
    if (f.exists()) f.inputStream().use { load(it) }
}
fun devProp(key: String): String = devProps.getProperty(key) ?: ""

composeCompiler {
    stabilityConfigurationFiles.add(rootProject.layout.projectDirectory.file("app/compose-stability.conf"))
}

java {
    toolchain { languageVersion = JavaLanguageVersion.of(17) }
}

android {
    namespace = "tv.roxy.tv"
    compileSdk = 37 // current AndroidX (okhttp-android 5.5, etc.) requires API 37+ to compile

    defaultConfig {
        applicationId = "tv.roxy.tv"
        minSdk = 28
        targetSdk = 36
        versionCode = 62
        versionName = "0.5.42"

        buildConfigField("String", "DEFAULT_SERVER_URL", "\"\"")
        buildConfigField("String", "DEFAULT_USERNAME", "\"\"")
        buildConfigField("String", "DEFAULT_PASSWORD", "\"\"")

        ndk {
            // libmpv AAR ships four ABIs; TVs are arm. Drop x86 to keep the APK small.
            abiFilters += listOf("arm64-v8a", "armeabi-v7a")
        }
    }

    buildTypes {
        debug {
            applicationIdSuffix = ".debug"
            buildConfigField("String", "DEFAULT_SERVER_URL", "\"${devProp("server.url")}\"")
            buildConfigField("String", "DEFAULT_USERNAME", "\"${devProp("server.username")}\"")
            buildConfigField("String", "DEFAULT_PASSWORD", "\"${devProp("server.password")}\"")
        }
        release {
            // Never ship dev defaults (spike.properties) in a published build.
            buildConfigField("String", "DEFAULT_SERVER_URL", "\"\"")
            buildConfigField("String", "DEFAULT_USERNAME", "\"\"")
            buildConfigField("String", "DEFAULT_PASSWORD", "\"\"")
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
        // The debug app (same id and key, so it installs over it and keeps the
        // sign-in) built the release way: R8, no debug checks. What the TV is measured with.
        create("perf") {
            initWith(getByName("debug"))
            isDebuggable = false
            // perf builds are what gets published to the in-app update channel: the dev
            // defaults from spike.properties (debug only) must never end up in one.
            buildConfigField("String", "DEFAULT_SERVER_URL", "\"\"")
            buildConfigField("String", "DEFAULT_USERNAME", "\"\"")
            buildConfigField("String", "DEFAULT_PASSWORD", "\"\"")
            isMinifyEnabled = true
            isShrinkResources = true
            signingConfig = signingConfigs.getByName("debug")
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            matchingFallbacks += listOf("release", "debug")
        }
    }

    buildFeatures {
        buildConfig = true
        compose = true
    }

    // Fonts stay uncompressed in the APK: libmpv's fonts are copied out at first start, and
    // a stored asset copies without inflating.
    androidResources {
        noCompress += listOf("ttf", "otf", "ttc")
    }

    packaging {
        resources { excludes += "/META-INF/{AL2.0,LGPL2.1}" }
        jniLibs { useLegacyPackaging = false }
    }
}

dependencies {
    implementation(libs.androidx.core)
    // Installs src/main/baseline-prof.txt on sideloaded installs (no Play Store to do it).
    implementation("androidx.profileinstaller:profileinstaller:1.4.1")
    // Sign in a TV: the phone's QR scanner (CameraX + ML Kit's bundled barcode model).
    implementation(libs.androidx.camera.camera2)
    implementation(libs.androidx.camera.lifecycle)
    implementation(libs.androidx.camera.view)
    implementation(libs.mlkit.barcode.scanning)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.compose.runtime)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.material.icons)
    implementation(libs.androidx.tv.material)
    // Android TV's Watch Next store (the launcher's "Continue watching" row).
    implementation("androidx.tvprovider:tvprovider:1.0.0")
    implementation(libs.androidx.navigation.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.datastore.preferences)
    implementation(libs.coil.compose)
    implementation(libs.coil.network.okhttp)
    implementation(libs.okhttp)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)
    // libmpv: our build of libmpv-android v1.0.0 with libplacebo 7.387.0 (GL sync leak and AV1
    // film-grain fixes) and libass 0.17.5; mpv 0.41.0, same MPVLib API. Recipe: tools/build-libmpv.md.
    implementation(files("libs/libmpv-roxy-1.0.0-pl7.387.aar"))
    implementation(libs.timber)

    debugImplementation(libs.androidx.compose.ui.tooling)
}
