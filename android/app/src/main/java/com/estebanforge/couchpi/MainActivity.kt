package com.estebanforge.couchpi

import android.annotation.SuppressLint
import android.app.Activity
import android.content.ActivityNotFoundException
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
import android.view.HapticFeedbackConstants
import android.view.View
import android.view.WindowInsets
import android.window.OnBackInvokedCallback
import android.webkit.JavascriptInterface
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import android.graphics.drawable.GradientDrawable
import android.view.Gravity
import android.view.ViewGroup
import android.view.inputmethod.EditorInfo
import org.json.JSONObject

// ADR-0003: one Activity, one WebView, full-screen. An icon and a container,
// nothing more: no IPC bridge, no bundled assets, no FCM. The URL is set on
// first launch and persisted on device; there is no default, so the first-run
// card asks for it.
class MainActivity : Activity() {

    private val prefs by lazy { getSharedPreferences("couch-pi", Context.MODE_PRIVATE) }
    private var web: WebView? = null

    // The configured server origin: the only URL allowed to render in
    // the bridged WebView (see shouldOverrideUrlLoading).
    private var serverUrl: String? = null

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

    // First-launch card: enter the server URL, then connect.
    // Native because the WebView needs the URL before it can load anything;
    // styled after the app's default cobalt scheme since it paints before
    // any web theme could.
    private fun firstRunView(): View {
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setPadding(dp(28), dp(24), dp(28), dp(24))
        }
        content.addView(ImageView(this).apply {
            setImageResource(R.drawable.ic_launcher_foreground)
            layoutParams = LinearLayout.LayoutParams(dp(96), dp(96))
        })
        content.addView(TextView(this).apply {
            text = "Pi Couch"
            textSize = 26f
            setTextColor(Color.parseColor("#d7deea"))
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT,
            ).apply { topMargin = dp(16) }
        })
        content.addView(TextView(this).apply {
            text = "In your Tailscale app, open the machine running Pi Couch and copy its MagicDNS name. Paste it here."
            textSize = 15f
            setTextColor(Color.parseColor("#62747f"))
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT,
            ).apply { topMargin = dp(8) }
        })
        val input = EditText(this).apply {
            hint = "box.example.ts.net"
            setSingleLine(true)
            imeOptions = EditorInfo.IME_ACTION_GO
            setTextColor(Color.parseColor("#d7deea"))
            setHintTextColor(Color.parseColor("#62747f"))
            textSize = 15f
            background = GradientDrawable().apply {
                setColor(Color.parseColor("#11242e"))
                cornerRadius = dp(12).toFloat()
            }
            setPadding(dp(16), dp(14), dp(16), dp(14))
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT,
            ).apply { topMargin = dp(28) }
        }
        content.addView(input)
        // The port has a project default (the README's serve example), so the
        // main flow never asks for it; Advanced overrides it without leaving
        // the screen.
        val advanced = TextView(this).apply {
            text = "Advanced"
            textSize = 13f
            setTextColor(Color.parseColor("#62747f"))
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT,
            ).apply { topMargin = dp(14) }
        }
        content.addView(advanced)
        val portRow = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            visibility = View.GONE
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT,
            ).apply { topMargin = dp(10) }
        }
        portRow.addView(TextView(this).apply {
            text = "Port"
            textSize = 14f
            setTextColor(Color.parseColor("#62747f"))
        })
        val portInput = EditText(this).apply {
            setText("48443")
            setSingleLine(true)
            inputType = android.text.InputType.TYPE_CLASS_NUMBER
            setTextColor(Color.parseColor("#d7deea"))
            textSize = 14f
            background = GradientDrawable().apply {
                setColor(Color.parseColor("#11242e"))
                cornerRadius = dp(10).toFloat()
            }
            setPadding(dp(12), dp(10), dp(12), dp(10))
            layoutParams = LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f).apply {
                marginStart = dp(12)
            }
        }
        portRow.addView(portInput)
        content.addView(portRow)
        val tokenInput = EditText(this).apply {
            hint = "Access token (optional)"
            setSingleLine(true)
            inputType = android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD
            setTextColor(Color.parseColor("#d7deea"))
            setHintTextColor(Color.parseColor("#62747f"))
            textSize = 15f
            background = GradientDrawable().apply {
                setColor(Color.parseColor("#11242e"))
                cornerRadius = dp(12).toFloat()
            }
            setPadding(dp(16), dp(14), dp(16), dp(14))
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT,
            ).apply { topMargin = dp(12) }
        }
        content.addView(tokenInput)
        advanced.setOnClickListener {
            portRow.visibility = if (portRow.visibility == View.VISIBLE) View.GONE else View.VISIBLE
        }
        val connect = Button(this).apply {
            text = getString(R.string.connect)
            isAllCaps = false
            textSize = 16f
            setTextColor(Color.parseColor("#071219"))
            background = GradientDrawable().apply {
                setColor(Color.parseColor("#409dd4"))
                cornerRadius = dp(12).toFloat()
            }
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT,
            ).apply { topMargin = dp(12) }
        }
        content.addView(connect)
        connect.setOnClickListener {
            // The Tailscale app's copy button hands over a bare MagicDNS
            // name; scheme and port are ours to compose.
            val host = input.text.toString().trim().trimEnd('/')
            val url = when {
                host.isEmpty() -> ""
                host.startsWith("https://") || host.startsWith("http://") -> host
                host.contains(":") -> "https://" + host
                else -> "https://" + host + ":" + portInput.text.toString().trim()
            }
            if (url.startsWith("https://") || url.startsWith("http://")) {
                prefs.edit().putString(PREF_URL, url).apply()
                setTokenCookie(url, tokenInput.text.toString().trim())
                attachWeb(url)
            } else {
                input.error = getString(R.string.bad_url)
            }
        }
        input.setOnEditorActionListener { _, actionId, _ ->
            if (actionId == EditorInfo.IME_ACTION_GO) { connect.performClick(); true } else false
        }
        // fillViewport + centered column: the keyboard push-up lands on the
        // ScrollView instead of burying the button.
        return ScrollView(this).apply {
            setBackgroundColor(Color.parseColor("#0b1c24"))
            isFillViewport = true
        }.also {
            it.addView(content, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    // The SPA never handles the token itself: it rides as a cookie on every
    // same-origin request (page, fetch, and EventSource alike), so the
    // server's auth gate passes with zero web changes. A blank token clears
    // any earlier cookie so reconnecting tokenless stays tokenless.
    private fun setTokenCookie(url: String, token: String) {
        val origin = android.net.Uri.parse(url).let { u ->
            "${u.scheme}://${u.host}" + if (u.port != -1) ":${u.port}" else ""
        }
        val manager = android.webkit.CookieManager.getInstance()
        if (token.isEmpty()) {
            manager.setCookie(origin, "picouch_token=; Path=/; Max-Age=0")
        } else {
            // HttpOnly keeps the token out of page JS (a foreign page in
            // the WebView or an XSS in the SPA never reads it); the
            // browser still sends it with every request. The token rides
            // a cookie attribute, so stray separators would break the
            // header - percent-escape anything outside the token alphabet.
            val safe = token.filter { it.isLetterOrDigit() || it in "-_~=./" }
            if (safe.length != token.length) {
                Log.w("couch-pi", "token carried unusable characters; cookie not set")
            } else {
                manager.setCookie(origin, "picouch_token=$safe; Path=/; Secure; HttpOnly; SameSite=Lax")
            }
        }
        manager.flush()
    }

    private fun attachWeb(url: String) {
        val view = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccess = false
            settings.allowContentAccess = false
            // Remote WebView debugging over adb (chrome://inspect). Debug
            // builds only: a release build must not offer its grids to
            // any USB-connected machine.
            WebView.setWebContentsDebuggingEnabled(BuildConfig.DEBUG)
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
        serverUrl = url
        view.addJavascriptInterface(speech, "CouchPiSpeech")
        view.addJavascriptInterface(haptics, "CouchPiHaptics")
        setContentView(view)
        setupBackInvocation()
        @Suppress("DEPRECATION") // pre-35 devices; ignored on 35+
        window.statusBarColor = Color.TRANSPARENT
        view.loadUrl(url)
    }

    // Native dictation: the Android WebView ships no Web Speech API, so
    // the shell exposes the platform SpeechRecognizer to the page. Same
    // contract the web path offers: finals only, an end, an error. The
    // page installs window.__couchPiDictation callbacks before start().
    private val speech = SpeechBridge()
    private val haptics = HapticsBridge()

    private inner class HapticsBridge {
        @JavascriptInterface
        fun tick() {
            runOnUiThread {
                web?.performHapticFeedback(HapticFeedbackConstants.CLOCK_TICK)
            }
        }

        @JavascriptInterface
        fun confirm() {
            runOnUiThread {
                web?.performHapticFeedback(HapticFeedbackConstants.CONFIRM)
            }
        }

        @JavascriptInterface
        fun longPress() {
            runOnUiThread {
                web?.performHapticFeedback(HapticFeedbackConstants.LONG_PRESS)
            }
        }
    }

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
            // The bridges stay attached to this WebView for its whole
            // life, so only the configured origin may render here; every
            // other link (markdown, target=_blank, redirects) hands off
            // to the browser. NavGuard fails closed.
            override fun shouldOverrideUrlLoading(
                view: WebView,
                request: WebResourceRequest,
            ): Boolean {
                if (!NavGuard.opensExternally(serverUrl, request.url.toString())) return false
                openExternally(request.url)
                return true
            }

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

    private fun openExternally(url: Uri) {
        try {
            startActivity(Intent(Intent.ACTION_VIEW, url).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        } catch (_: ActivityNotFoundException) {
            // No browser installed: dropping the link beats loading it
            // next to the mic bridge.
        }
    }

    private fun loadErrorHtml(failedUrl: String): String {
        // The failed URL rides a data attribute, escaped, never
        // interpolated into the onclick string: a quote in it used to
        // break out of the attribute and run script.
        val target = NavGuard.htmlAttrEscape(failedUrl)
        val body = "<html><body style='background:#0f1115;color:#e5e7eb;" +
            "font-family:sans-serif;display:flex;align-items:center;" +
            "justify-content:center;height:100%;margin:0'>" +
            "<div style='text-align:center'><p>Pi Couch is unreachable.</p>" +
            "<p style='opacity:0.6;font-size:0.9em'>Check the tailnet, then reload.</p>" +
            "<p><button data-href=\"$target\" onclick=\"location.href=this.dataset.href\" " +
            "style='margin-top:16px;padding:10px 24px;border-radius:8px;border:0;" +
            "background:#4d9abf;color:#0f1115'>Reload</button></p></div></body></html>"
        return body
    }

    private fun setupBackInvocation() {
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.TIRAMISU) {
            onBackInvokedDispatcher.registerOnBackInvokedCallback(
                android.window.OnBackInvokedDispatcher.PRIORITY_DEFAULT,
                OnBackInvokedCallback {
                    val w = web
                    if (w != null && w.canGoBack()) {
                        w.goBack()
                    } else {
                        finish()
                    }
                }
            )
        }
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
        const val PREF_URL = "url"
        const val speechReq = 7002

        private const val TAG = "CouchPiSpeech"
    }
}
