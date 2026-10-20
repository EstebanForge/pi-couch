package com.estebanforge.couchpi

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import android.util.Log
import android.view.View
import android.view.WindowInsets
import android.webkit.JavascriptInterface
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import org.json.JSONObject

// ADR-0003: one Activity, one WebView, full-screen. An icon and a container,
// nothing more: no IPC bridge, no bundled assets, no FCM. The URL is set on
// first launch and persisted on device; the default is the tailscale serve
// URL, which reaches the server both on the home LAN (peer-to-peer) and away
// on mobile data (tailscale). The server binds loopback only, so there is no
// second address to reach.
class MainActivity : Activity() {

    private val prefs by lazy { getSharedPreferences("couch-pi", Context.MODE_PRIVATE) }
    private var web: WebView? = null

    // Pending file-chooser callback: the WebView hands it over in
    // onShowFileChooser and demands exactly one onReceiveValue, on pick
    // or on cancel, or every later tap on the attach button dead-ends.
    private var fileCallback: ValueCallback<Array<Uri>>? = null

    private val fileReq = 7001

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val saved = prefs.getString(PREF_URL, null)
        if (saved == null) {
            setContentView(firstRunView())
        } else {
            attachWeb(saved)
        }
    }

    // First-launch card: confirm or replace the default URL, then connect.
    private fun firstRunView(): View {
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(Color.parseColor("#0f1115"))
            setPadding(dp(24), dp(96), dp(24), dp(24))
        }
        val input = EditText(this).apply {
            hint = DEFAULT_URL
            setText(DEFAULT_URL)
            setSingleLine(true)
        }
        val connect = Button(this).apply { text = getString(R.string.connect) }
        box.addView(input)
        box.addView(connect)
        connect.setOnClickListener {
            val url = input.text.toString().trim().trimEnd('/')
            if (url.startsWith("https://") || url.startsWith("http://")) {
                prefs.edit().putString(PREF_URL, url).apply()
                attachWeb(url)
            } else {
                input.error = getString(R.string.bad_url)
            }
        }
        return box
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun attachWeb(url: String) {
        val view = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccess = false
            settings.allowContentAccess = false
            // Remote WebView debugging over adb (chrome://inspect). Debug
            // builds only; it is the precision instrument for phone-side
            // web bugs and stays out of any future signed release.
            WebView.setWebContentsDebuggingEnabled(true)
            webViewClient = safeAreaClient() // stay in-app; inject real insets
            // The file input needs a WebChromeClient: without
            // onShowFileChooser the WebView no-ops the tap and the
            // mobile file/gallery picker never opens.
            webChromeClient = object : WebChromeClient() {
                override fun onShowFileChooser(
                    webView: WebView?,
                    callback: ValueCallback<Array<Uri>>,
                    params: FileChooserParams,
                ): Boolean {
                    fileCallback?.onReceiveValue(null)
                    fileCallback = callback
                    // createIntent carries the page's accept list (no
                    // accept attr means any file), CATEGORY_OPENABLE,
                    // and multi-select from the input's multiple attr.
                    startActivityForResult(params.createIntent(), fileReq)
                    return true
                }
            }
            setBackgroundColor(Color.parseColor("#0f1115"))
        }
        // Edge-to-edge at targetSdk 35: the WebView stays full-bleed so the
        // page background flows under the transparent status bar. The page
        // offsets its own content via env(safe-area-inset-*) (viewport-fit
        // cover), so the shell applies no padding of its own.
        web = view
        view.addJavascriptInterface(speech, "CouchPiSpeech")
        setContentView(view)
        @Suppress("DEPRECATION") // pre-35 devices; ignored on 35+
        window.statusBarColor = Color.TRANSPARENT
        view.loadUrl(url)
    }

    // Native dictation: the Android WebView ships no Web Speech API, so
    // the shell exposes the platform SpeechRecognizer to the page. Same
    // contract the web path offers: finals only, an end, an error. The
    // page installs window.__couchPiDictation callbacks before start().
    private val speech = SpeechBridge()

    private var pendingSpeechLang: String? = null

    private inner class SpeechBridge {
        private var recognizer: SpeechRecognizer? = null

        // True while the page wants listening: the recognizer restarts
        // per phrase and silence is retried instead of reported.
        private var wanting = false

        @JavascriptInterface
        fun available(): Boolean = SpeechRecognizer.isRecognitionAvailable(this@MainActivity)

        @JavascriptInterface
        fun start(lang: String) {
            runOnUiThread {
                if (checkSelfPermission(android.Manifest.permission.RECORD_AUDIO) !=
                    PackageManager.PERMISSION_GRANTED
                ) {
                    pendingSpeechLang = lang
                    requestPermissions(arrayOf(android.Manifest.permission.RECORD_AUDIO), speechReq)
                    return@runOnUiThread
                }
                begin(lang)
            }
        }

        // The permission-grant path lands here too: listening starts only
        // through this gate.
        fun begin(lang: String) {
            wanting = true
            listen(lang)
        }

        @JavascriptInterface
        fun stop() {
            runOnUiThread {
                wanting = false
                recognizer?.stopListening()
            }
        }

        private fun listen(lang: String) {
            recognizer?.destroy()
            recognizer = null
            // The service needs a beat to release the mic after a long
            // session; an instant re-arm races it into ERROR_CLIENT.
            Handler(Looper.getMainLooper()).postDelayed({ if (wanting) arm(lang) }, 250)
        }

        private fun arm(lang: String) {
            if (!wanting) return
            val rec = SpeechRecognizer.createSpeechRecognizer(this@MainActivity)
            recognizer = rec
            rec.setRecognitionListener(
                object : RecognitionListener {
                    override fun onResults(results: Bundle) {
                        val text = results
                            .getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)
                            ?.firstOrNull()
                        Log.i(TAG, "results: wanting=$wanting text=${text?.take(40)}")
                        if (!text.isNullOrBlank()) emit("onFinal", text)
                        if (wanting) listen(lang) else finish()
                    }

                    override fun onError(error: Int) {
                        Log.w(TAG, "recognition error $error (wanting=$wanting)")
                        // Silence while the page still wants listening is a
                        // pause, not a failure: re-arm and keep waiting.
                        // CLIENT and RECOGNIZER_BUSY are restart races, not
                        // real failures, while a session is still wanted.
                        if (wanting && (
                                error == SpeechRecognizer.ERROR_NO_MATCH ||
                                    error == SpeechRecognizer.ERROR_SPEECH_TIMEOUT ||
                                    error == SpeechRecognizer.ERROR_CLIENT ||
                                    error == SpeechRecognizer.ERROR_RECOGNIZER_BUSY
                                )
                        ) {
                            listen(lang)
                            return
                        }
                        wanting = false
                        emit("onError", null)
                        finish()
                    }

                    override fun onPartialResults(partialResults: Bundle?) {}

                    override fun onBeginningOfSpeech() {}

                    override fun onEndOfSpeech() {}

                    override fun onReadyForSpeech(params: Bundle?) {}

                    override fun onBufferReceived(buffer: ByteArray?) {}

                    override fun onRmsChanged(rmsdB: Float) {}

                    override fun onEvent(eventType: Int, params: Bundle?) {}
                },
            )
            val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
                putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
                putExtra(RecognizerIntent.EXTRA_LANGUAGE, lang)
            }
            rec.startListening(intent)
        }

        private fun finish() {
            recognizer?.destroy()
            recognizer = null
            web?.evaluateJavascript("window.__couchPiDictation&&window.__couchPiDictation.onEnd()", null)
        }

        private fun emit(fn: String, arg: String?) {
            val a = arg?.let { JSONObject.quote(it) } ?: ""
            web?.evaluateJavascript("window.__couchPiDictation&&window.__couchPiDictation.$fn($a)", null)
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray,
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode != speechReq) return
        val lang = pendingSpeechLang
        pendingSpeechLang = null
        if (grantResults.firstOrNull() == PackageManager.PERMISSION_GRANTED && lang != null) {
            speech.begin(lang)
        } else {
            web?.evaluateJavascript("window.__couchPiDictation&&window.__couchPiDictation.onError()", null)
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != fileReq) return
        val cb = fileCallback ?: return
        fileCallback = null
        if (resultCode != RESULT_OK || data == null) {
            cb.onReceiveValue(null) // picker cancelled: release, don't attach
            return
        }
        val uris = mutableListOf<Uri>()
        val clip = data.clipData
        if (clip != null) {
            for (i in 0 until clip.itemCount) uris.add(clip.getItemAt(i).uri)
        } else if (data.data != null) {
            uris.add(data.data!!)
        }
        cb.onReceiveValue(uris.toTypedArray())
    }

    // Chromium WebView keeps env(safe-area-inset-*) at 0, so pass the real
    // window insets (in CSS px) to the page on every load. Datastar patches
    // keep the injected documentElement vars; navigations refire this hook.
    private fun safeAreaClient(): WebViewClient {
        return object : WebViewClient() {
            override fun onPageFinished(view: WebView, url: String?) {
                val ins = view.rootWindowInsets?.getInsets(
                    WindowInsets.Type.statusBars() or WindowInsets.Type.navigationBars()
                ) ?: return
                val d = resources.displayMetrics.density
                val js = "document.documentElement.style.setProperty" +
                    "(\"--safe-top\",\"${ins.top / d}px\");" +
                    "document.documentElement.style.setProperty" +
                    // Halved: the full gesture-bar inset reads as a huge
                    // empty strip under the composer; half clears the bar
                    // while keeping the app close to the bottom edge.
                    "(\"--safe-bottom\",\"${ins.bottom / d / 2}px\")"
                view.evaluateJavascript(js, null)
            }

            // A failed load (tailnet down, server stopped) must not leave a
            // bare dark page: say what happened and offer a reload.
            override fun onReceivedError(
                view: WebView,
                req: WebResourceRequest,
                err: WebResourceError,
            ) {
                if (req.isForMainFrame) {
                    // loadDataWithBaseURL, not loadData: loadData parses its
                    // payload as a URL, so the hex colors' '#' read as URL
                    // fragments and the card rendered as an empty document.
                    view.loadDataWithBaseURL(
                        null,
                        loadErrorHtml(req.url.toString()),
                        "text/html",
                        "utf-8",
                        null,
                    )
                }
            }
        }
    }

    private fun loadErrorHtml(failedUrl: String): String {
        val body = "<html><body style='background:#0f1115;color:#e5e7eb;" +
            "font-family:sans-serif;display:flex;align-items:center;" +
            "justify-content:center;height:100%;margin:0'>" +
            "<div style='text-align:center'><p>Couch Pi is unreachable.</p>" +
            "<p style='opacity:0.6;font-size:0.9em'>Check the tailnet, then reload.</p>" +
            "<p><button onclick=\"location.href='$failedUrl'\" " +
            "style='margin-top:16px;padding:10px 24px;border-radius:8px;border:0;" +
            "background:#4d9abf;color:#0f1115'>Reload</button></p></div></body></html>"
        return body
    }

    // Back walks browser history inside the shell before leaving the app.
    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        val w = web
        if (w != null && w.canGoBack()) {
            w.goBack()
        } else {
            @Suppress("DEPRECATION")
            super.onBackPressed()
        }
    }

    private fun dp(v: Int): Int = (v * resources.displayMetrics.density).toInt()

    private companion object {
        // Tailscale serve fronts the loopback-only Go server; the tailnet is
        // the boundary (project P1). Same URL works on LAN and off-site.
        // :8443 — Wicket stacks publish 0.0.0.0:443 machine-wide.
        const val DEFAULT_URL = "https://box.example.ts.net:8443"
        const val PREF_URL = "url"
        const val speechReq = 7002

        private const val TAG = "CouchPiSpeech"
    }
}
