// Couch Pi Android shell (ADR-0003): thin WebView wrapper, no frameworks.
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "couch-pi"

include(":app")
