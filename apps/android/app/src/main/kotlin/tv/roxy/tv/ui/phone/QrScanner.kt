package tv.roxy.tv.ui.phone

import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.Camera
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.outlined.FlashlightOff
import androidx.compose.material.icons.outlined.FlashlightOn
import androidx.compose.material.icons.outlined.ZoomIn
import androidx.compose.material.icons.outlined.ZoomOut
import androidx.compose.material3.Icon
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.tv.material3.Text
import com.google.mlkit.vision.barcode.BarcodeScannerOptions
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage
import timber.log.Timber
import tv.roxy.tv.ui.theme.RoxyColors
import java.util.concurrent.Executors

/**
 * A full-screen QR scanner: the camera behind a dimmed frame with accent corners, pinch
 * or the slider to zoom, a torch, and Close. CameraX for the picture, ML Kit's bundled
 * barcode model for the decoding (on the phone, no Play services module to fetch).
 * [onResult] gets the first QR's text once; [onClose] when the person backs out.
 */
@Composable
fun QrScanner(onResult: (String) -> Unit, onClose: () -> Unit, onError: (String) -> Unit) {
    val context = LocalContext.current
    var granted by remember { mutableStateOf(ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) }
    var refused by remember { mutableStateOf(false) }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok -> granted = ok; refused = !ok }
    LaunchedEffect(Unit) { if (!granted) ask.launch(Manifest.permission.CAMERA) }
    BackHandler(onBack = onClose)

    Box(modifier = Modifier.fillMaxSize().background(Color.Black)) {
        when {
            granted -> CameraView(onResult = onResult, onError = onError)
            refused -> Column(modifier = Modifier.align(Alignment.Center).padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                Text("Camera access is off", fontSize = 18.sp, fontWeight = FontWeight.Bold, color = Color.White)
                Text("Allow the camera for Roxy in the phone's settings, or close this and type the code from the TV.", fontSize = 14.sp, lineHeight = 20.sp, color = Color.White.copy(alpha = 0.75f), textAlign = TextAlign.Center, modifier = Modifier.padding(top = 8.dp, bottom = 20.dp))
                PlayButton("Ask again", pill = true, container = RoxyColors.accent, content = OnBrand, onClick = { refused = false; ask.launch(Manifest.permission.CAMERA) })
            }
        }
        // Close, over everything.
        Box(
            modifier = Modifier.statusBarsPadding().padding(12.dp).size(44.dp).clip(CircleShape).background(Color.Black.copy(alpha = 0.55f)).clickable(onClick = onClose),
            contentAlignment = Alignment.Center,
        ) { Icon(Icons.Filled.Close, contentDescription = "Close", tint = Color.White) }
    }
}

@Composable
private fun CameraView(onResult: (String) -> Unit, onError: (String) -> Unit) {
    val context = LocalContext.current
    val lifecycle = LocalLifecycleOwner.current
    val previewView = remember { PreviewView(context).apply { scaleType = PreviewView.ScaleType.FILL_CENTER } }
    var camera by remember { mutableStateOf<Camera?>(null) }
    var zoom by remember { mutableFloatStateOf(0f) } // linear 0..1
    var torch by remember { mutableStateOf(false) }
    var delivered by remember { mutableStateOf(false) }

    DisposableEffect(lifecycle) {
        val executor = Executors.newSingleThreadExecutor()
        val scanner = BarcodeScanning.getClient(BarcodeScannerOptions.Builder().setBarcodeFormats(Barcode.FORMAT_QR_CODE).build())
        val providerFuture = ProcessCameraProvider.getInstance(context)
        providerFuture.addListener({
            runCatching {
                val provider = providerFuture.get()
                val preview = Preview.Builder().build().also { it.surfaceProvider = previewView.surfaceProvider }
                val analysis = ImageAnalysis.Builder().setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST).build()
                analysis.setAnalyzer(executor) { proxy ->
                    val media = proxy.image
                    if (media == null || delivered) { proxy.close(); return@setAnalyzer }
                    scanner.process(InputImage.fromMediaImage(media, proxy.imageInfo.rotationDegrees))
                        .addOnSuccessListener { codes ->
                            val text = codes.firstNotNullOfOrNull { it.rawValue }
                            if (text != null && !delivered) { delivered = true; onResult(text) }
                        }
                        .addOnCompleteListener { proxy.close() }
                }
                provider.unbindAll()
                camera = provider.bindToLifecycle(lifecycle, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis)
            }.onFailure { t -> Timber.w(t, "camera"); onError("The camera could not start (${t.message}).") }
        }, ContextCompat.getMainExecutor(context))
        onDispose {
            runCatching { providerFuture.get().unbindAll() }
            scanner.close()
            executor.shutdown()
        }
    }
    LaunchedEffect(camera, zoom) { camera?.cameraControl?.setLinearZoom(zoom) }
    LaunchedEffect(camera, torch) { camera?.cameraControl?.enableTorch(torch) }

    Box(
        modifier = Modifier.fillMaxSize().pointerInput(Unit) {
            // Pinch to zoom: spread the fingers to get closer to a TV across the room.
            detectTransformGestures { _, _, scale, _ -> zoom = (zoom + (scale - 1f) * 0.6f).coerceIn(0f, 1f) }
        },
    ) {
        AndroidView(factory = { previewView }, modifier = Modifier.fillMaxSize())
        ScanFrame()
        Column(
            modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().navigationBarsPadding().padding(horizontal = 24.dp, vertical = 28.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text("Point at the QR code on the TV", fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = Color.White)
            Text("Pinch or slide to zoom", fontSize = 13.sp, color = Color.White.copy(alpha = 0.7f), modifier = Modifier.padding(top = 4.dp, bottom = 18.dp))
            Row(
                modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(28.dp)).background(Color.Black.copy(alpha = 0.55f)).padding(horizontal = 14.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(Icons.Outlined.ZoomOut, contentDescription = "Zoom out", tint = Color.White, modifier = Modifier.size(22.dp).clickable { zoom = (zoom - 0.15f).coerceAtLeast(0f) })
                Slider(
                    value = zoom, onValueChange = { zoom = it }, modifier = Modifier.weight(1f).padding(horizontal = 10.dp),
                    colors = SliderDefaults.colors(thumbColor = Color.White, activeTrackColor = RoxyColors.accent, inactiveTrackColor = Color.White.copy(alpha = 0.3f)),
                )
                Icon(Icons.Outlined.ZoomIn, contentDescription = "Zoom in", tint = Color.White, modifier = Modifier.size(22.dp).clickable { zoom = (zoom + 0.15f).coerceAtMost(1f) })
                Spacer(Modifier.width(14.dp))
                Box(
                    modifier = Modifier.size(40.dp).clip(CircleShape).background(if (torch) Color.White else Color.White.copy(alpha = 0.15f)).clickable { torch = !torch },
                    contentAlignment = Alignment.Center,
                ) { Icon(if (torch) Icons.Outlined.FlashlightOn else Icons.Outlined.FlashlightOff, contentDescription = "Light", tint = if (torch) Color.Black else Color.White, modifier = Modifier.size(20.dp)) }
            }
            Spacer(Modifier.height(4.dp))
        }
    }
}

/** The dimmed screen with a clear rounded square in the middle and accent corner brackets. */
@Composable
private fun ScanFrame() {
    Canvas(modifier = Modifier.fillMaxSize().graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }) {
        val side = size.minDimension * 0.68f
        val topLeft = Offset((size.width - side) / 2f, (size.height - side) / 2f - size.height * 0.06f)
        val radius = 28.dp.toPx()
        drawRect(Color.Black.copy(alpha = 0.55f))
        drawRoundRect(Color.Transparent, topLeft, Size(side, side), CornerRadius(radius), blendMode = BlendMode.Clear)
        val len = side * 0.16f
        val w = 5.dp.toPx()
        val c = RoxyColors.accent
        val l = topLeft.x; val t = topLeft.y; val r = l + side; val b = t + side
        // Each corner: an arc with two straight arms.
        fun corner(cx: Float, cy: Float, sx: Float, sy: Float, start: Float) {
            drawArc(c, start, 90f, false, Offset(if (sx > 0) cx else cx - 2 * radius, if (sy > 0) cy else cy - 2 * radius), Size(2 * radius, 2 * radius), style = Stroke(w, cap = StrokeCap.Round))
            drawLine(c, Offset(cx + sx * radius, cy), Offset(cx + sx * (radius + len), cy), w, StrokeCap.Round)
            drawLine(c, Offset(cx, cy + sy * radius), Offset(cx, cy + sy * (radius + len)), w, StrokeCap.Round)
        }
        corner(l, t, 1f, 1f, 180f)
        corner(r, t, -1f, 1f, 270f)
        corner(r, b, -1f, -1f, 0f)
        corner(l, b, 1f, -1f, 90f)
    }
}
