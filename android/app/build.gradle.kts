import java.util.Properties
import java.io.FileInputStream

plugins {
    alias(libs.plugins.androidApplication)
    alias(libs.plugins.kotlinAndroid)
}

// Release signing reads a properties file held OUTSIDE the repo (or via the
// PI_COUCH_SIGNING_PROPS env var, how CI would pass it); no file, no signing
// config, and release falls back to the debug key so any machine can build.
val signingPropsFile = System.getenv("PI_COUCH_SIGNING_PROPS")?.let { rootProject.file(it) }
    ?: rootProject.file("keystore.properties")
val signingProps = Properties().apply {
    if (signingPropsFile.exists()) FileInputStream(signingPropsFile).use { load(it) }
}

android {
    namespace = "com.estebanforge.couchpi"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.estebanforge.couchpi"
        minSdk = 32
        targetSdk = 35
        versionCode = 10
        versionName = "20261029"
    }

    signingConfigs {
        if (signingPropsFile.exists()) {
            create("release") {
                storeFile = file(signingProps["storeFile"] as String)
                storePassword = signingProps["storePassword"] as String
                keyAlias = signingProps["keyAlias"] as String
                keyPassword = signingProps["keyPassword"] as String
            }
        }
    }

    buildTypes {
        release {
            signingConfig = if (signingPropsFile.exists())
                signingConfigs.getByName("release") else signingConfigs.getByName("debug")
        }
    }

    buildFeatures {
        // BuildConfig.DEBUG gates the WebView remote debugging flag.
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    testImplementation("junit:junit:4.13.2")
}
