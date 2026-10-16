plugins {
    alias(libs.plugins.androidApplication)
    alias(libs.plugins.kotlinAndroid)
}

android {
    namespace = "com.estebanforge.couchpi"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.estebanforge.couchpi"
        minSdk = 32
        targetSdk = 35
        versionCode = 2
        versionName = "0.1.0+beta04"
    }

    buildTypes {
        release {
            // Debug-signed for sideloading; a release key comes with a
            // real distribution story, which does not exist yet.
            signingConfig = signingConfigs.getByName("debug")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}
