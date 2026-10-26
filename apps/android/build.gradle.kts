// Root build file. Plugins are declared here (apply false) and applied per module.
plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.kotlin.compose.compiler) apply false
    alias(libs.plugins.kotlin.serialization) apply false
}

tasks.register<Delete>("clean") {
    description = "Deletes all build artifacts."
    delete(rootProject.layout.buildDirectory)
}
