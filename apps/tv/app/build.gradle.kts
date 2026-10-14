import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose.compiler)
    alias(libs.plugins.kotlin.serialization)
}

// Optional dev defaults in apps/tv/spike.properties (git-ignored). They only prefill the
// login screen of the debug build; release builds get empty defaults.
val devProps = Properties().apply {
    val f = rootProject.file("spike.properties")
    if (f.exists()) f.inputStream().use { load(it) }
}
fun devProp(key: String): String = devProps.getProperty(key) ?: ""

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
        versionCode = 2
        versionName = "0.2.0"

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
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }

    buildFeatures {
        buildConfig = true
        compose = true
    }

    packaging {
        resources { excludes += "/META-INF/{AL2.0,LGPL2.1}" }
        jniLibs { useLegacyPackaging = false }
    }
}

dependencies {
    implementation(libs.androidx.core)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.compose.runtime)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.tv.material)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.datastore.preferences)
    implementation(libs.coil.compose)
    implementation(libs.coil.network.okhttp)
    implementation(libs.okhttp)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.libmpv)
    implementation(libs.timber)

    debugImplementation(libs.androidx.compose.ui.tooling)
}
