# SPDX-License-Identifier: Apache-2.0

# Keep the line numbers, so that a stack trace from a release build retraces.
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile

# The gomobile binding is reached through JNI in both directions. R8 cannot see
# those calls, so it must not rename or remove any of it.
-keep class go.** { *; }
-keep class io.github.lightheaded.mobile.** { *; }

# kotlinx.serialization generates a companion serializer per class. R8 finds the
# class through reflection, so a rule has to name the pattern.
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.**
-keepclassmembers class io.github.lightheaded.teha.** {
    *** Companion;
}
-keepclasseswithmembers class io.github.lightheaded.teha.** {
    kotlinx.serialization.KSerializer serializer(...);
}
-keep,includedescriptorclasses class io.github.lightheaded.teha.**$$serializer { *; }

# Tink, which androidx.security.crypto uses for EncryptedSharedPreferences,
# carries compile-time annotations that never ship in an artifact. R8 sees the
# references and stops the build over classes that cannot exist at runtime.
#
# The first build failed on exactly this:
#   Missing class com.google.errorprone.annotations.CanIgnoreReturnValue
#   (referenced from com.google.crypto.tink.KeysetManager and 52 other contexts)
#
# -dontwarn, not -keep: keeping them is impossible, because they are not there.
-dontwarn com.google.errorprone.annotations.**
-dontwarn com.google.j2objc.annotations.**
-dontwarn javax.annotation.**
