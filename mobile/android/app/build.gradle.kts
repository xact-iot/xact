import java.util.Properties
import java.security.KeyStore
import java.security.cert.X509Certificate

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

val releaseProperties = Properties()
val releasePropertiesFile = rootProject.file("key.properties")
if (releasePropertiesFile.isFile) releasePropertiesFile.inputStream().use { releaseProperties.load(it) }
val releaseSigningReady = listOf("storeFile", "storePassword", "keyAlias", "keyPassword")
    .all { !releaseProperties.getProperty(it).isNullOrBlank() }

android {
    namespace = "com.xact.iot.mobile"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        isCoreLibraryDesugaringEnabled = true
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        applicationId = "com.xact.iot.mobile"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    signingConfigs {
        if (releaseSigningReady) {
            create("production") {
                storeFile = rootProject.file(releaseProperties.getProperty("storeFile"))
                storePassword = releaseProperties.getProperty("storePassword")
                keyAlias = releaseProperties.getProperty("keyAlias")
                keyPassword = releaseProperties.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            signingConfig = if (releaseSigningReady) signingConfigs.getByName("production") else null
        }
    }
}

val validateProductionSigning by tasks.registering {
    doLast {
        check(releaseSigningReady) {
            "Release signing is required. Configure android/key.properties with a dedicated production keystore."
        }
        val keystoreFile = rootProject.file(releaseProperties.getProperty("storeFile"))
        check(keystoreFile.isFile && keystoreFile.name != "debug.keystore" &&
            releaseProperties.getProperty("keyAlias") != "androiddebugkey") {
            "Release builds must use a dedicated production signing key, not the Android debug key."
        }
        val keystore = KeyStore.getInstance(keystoreFile, releaseProperties.getProperty("storePassword").toCharArray())
        val certificate = keystore.getCertificate(releaseProperties.getProperty("keyAlias")) as? X509Certificate
        check(certificate != null && !certificate.subjectX500Principal.name.contains("CN=Android Debug", ignoreCase = true)) {
            "An Android debug signing certificate cannot be used for production."
        }
    }
}
tasks.configureEach {
    if (name == "preReleaseBuild") dependsOn(validateProductionSigning)
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}

dependencies {
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.4")
    implementation(platform("com.google.firebase:firebase-bom:34.15.0"))
    implementation("com.google.firebase:firebase-common")
}
