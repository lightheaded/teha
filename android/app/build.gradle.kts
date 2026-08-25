// SPDX-License-Identifier: Apache-2.0

import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ksp)
}

// The version base moves by hand. The build number comes from the CI run
// number, so a local build and a CI build never claim the same version.
val versionBase = "0.1.0-alpha01"
val buildNumber: Int? = System.getenv("TEHA_BUILD_NUMBER")?.toIntOrNull()
val resolvedVersionName = buildNumber?.let { "$versionBase.$it" } ?: versionBase

android {
    namespace = "io.github.lightheaded.teha"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.github.lightheaded.teha"
        minSdk = 26
        targetSdk = 36
        versionCode = buildNumber ?: 1
        versionName = resolvedVersionName
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    signingConfigs {
        create("release") {
            // Populated in CI from repository secrets, absent locally.
            val storePath = System.getenv("TEHA_KEYSTORE_PATH")
            if (!storePath.isNullOrBlank() && file(storePath).exists()) {
                storeFile = file(storePath)
                storePassword = System.getenv("TEHA_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("TEHA_KEY_ALIAS")
                keyPassword = System.getenv("TEHA_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            // A contributor with no keystore still builds. The APK is then
            // unsigned, and only a CI run produces an installable file.
            if (System.getenv("TEHA_KEYSTORE_PATH") != null) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
        debug {
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_21
        targetCompatibility = JavaVersion.VERSION_21
    }

    buildFeatures {
        compose = true
    }

    sourceSets {
        // java.srcDirs also feeds the Kotlin compilation. It exists on every
        // AGP version, which the kotlin accessor does not.
        getByName("main").java.srcDirs("src/main/kotlin")
        getByName("androidTest").java.srcDirs("src/androidTest/kotlin")
        // The parser corpus is the contract for every client. The instrumented
        // test reads it from the repository root, so one file feeds Go and
        // Kotlin.
        getByName("androidTest").assets.srcDir("$rootDir/../parser-fixtures")
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }

    // One universal APK. An ABI split saves about 8 MB and costs a second
    // download link, which Obtainium then has to choose between.
    splits {
        abi { isEnable = false }
    }

    lint {
        abortOnError = false
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_21)
    }
}

dependencies {
    // The gomobile binding. CI produces it. See android/README.md.
    implementation(fileTree(mapOf("dir" to "libs", "include" to listOf("*.aar"))))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.security.crypto)

    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.graphics)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons.extended)
    debugImplementation(libs.compose.ui.tooling)

    implementation(libs.room.runtime)
    implementation(libs.room.ktx)
    ksp(libs.room.compiler)

    implementation(libs.okhttp)
    implementation(libs.kotlinx.serialization.json)

    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.androidx.test.ext.junit)
}

// A missing binding produces hundreds of "unresolved reference" lines. This
// stops the build with the command that fixes it.
tasks.register("checkBinding") {
    doLast {
        val found = file("libs").listFiles()?.any { it.name.endsWith(".aar") } ?: false
        if (!found) {
            throw GradleException(
                "android/app/libs/teha.aar is missing. Build it from the repository root:\n" +
                    "  gomobile bind -target=android -androidapi 26 " +
                    "-javapkg io.github.lightheaded -o android/app/libs/teha.aar ./mobile"
            )
        }
    }
}
tasks.named("preBuild") { dependsOn("checkBinding") }

// The release workflow reads the version from here. A grep over this file
// couples the shell to the Kotlin syntax, and breaks without a warning.
tasks.register("printVersion") {
    val v = resolvedVersionName
    doLast { println(v) }
}
