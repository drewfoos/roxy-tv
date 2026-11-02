# libmpv JNI callbacks are invoked by name from native code.
-keep class dev.jdtech.mpv.** { *; }

# ML Kit (the phone's QR scanner for TV sign-in) builds its parts from registrars it finds
# by name in the manifest. Shrunk, the lookup came back empty and BarcodeScanning.getClient
# crashed with a NullPointerException in the optimised builds (perf, release).
-keep class com.google.mlkit.** { *; }
-keep class com.google.android.gms.internal.mlkit_vision_barcode.** { *; }
-keep class com.google.android.gms.internal.mlkit_vision_common.** { *; }
-keep class com.google.android.gms.internal.mlkit_common.** { *; }
-keep class com.google.firebase.components.** { *; }
-keep class * implements com.google.firebase.components.ComponentRegistrar { *; }
