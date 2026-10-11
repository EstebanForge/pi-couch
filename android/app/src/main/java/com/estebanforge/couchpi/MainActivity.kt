package com.estebanforge.couchpi

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Context
import android.graphics.Color
import android.os.Bundle
import android.view.View
import android.view.WindowInsets
import android.view.ViewGroup
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout

// ADR-0003: one Activity, one WebView, full-screen. An icon and a container,
// nothing more: no IPC bridge, no bundled assets, no FCM. The URL is set on
// first launch and persisted on device; the default is the tailscale serve
// URL, which reaches the server both on the home LAN (peer-to-peer) and away
// on mobile data (tailscale). The server binds loopback only, so there is no
// second address to reach.
class MainActivity : Activity() {

    private val prefs by lazy { getSharedPreferences("couch-pi", Context.MODE_PRIVATE) }
    private var web: WebView? = null

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
            webViewClient = WebViewClient() // stay in-app: no browser tabs
            setBackgroundColor(Color.parseColor("#0f1115"))
        }
        // Edge-to-edge is enforced at targetSdk 35, so the bars draw over the
        // app. Pad a wrapper (never the WebView itself: it ignores view
        // padding when compositing) by the real window insets. The APK is the
        // only client of this shell, so the safe area is always applied.
        val root = android.widget.FrameLayout(this)
        root.addView(
            view,
            ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
        )
        root.setOnApplyWindowInsetsListener { v, insets ->
            val bars = insets.getInsets(
                WindowInsets.Type.statusBars()
                    or WindowInsets.Type.navigationBars()
                    or WindowInsets.Type.displayCutout()
            )
            v.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            WindowInsets.CONSUMED
        }
        web = view
        setContentView(root)
        @Suppress("DEPRECATION") // pre-35 devices; ignored on 35+
        window.statusBarColor = Color.parseColor("#0f1115")
        view.loadUrl(url)
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
        const val DEFAULT_URL = "https://box.example.ts.net"
        const val PREF_URL = "url"
    }
}
