package com.estebanforge.couchpi

import java.net.URI

// The WebView keeps the dictation and haptics bridges attached for its
// whole life, so only the configured server origin may render inside
// it; any other URL hands off to the browser. Pure JVM so the unit
// tests need no Android emulator or Robolectric.
object NavGuard {

    // True when the URL must not load in the app WebView. Unparseable,
    // schemeless or non-http(s) targets fail closed to the browser.
    fun opensExternally(appOrigin: String?, url: String?): Boolean {
        val target = targetUri(url) ?: return true
        val scheme = target.scheme?.lowercase() ?: return true
        if (scheme != "https" && scheme != "http") return true
        val host = target.host?.lowercase() ?: return true
        val origin = targetUri(appOrigin) ?: return true
        if (origin.scheme?.lowercase() != scheme) return true
        if (origin.host?.lowercase() != host) return true
        return defaultPort(scheme, origin.port) != defaultPort(scheme, target.port)
    }

    // HTML attribute context: escapes the five characters that can end
    // an attribute or open a tag, so a failed URL can never break out of
    // the error page's markup.
    fun htmlAttrEscape(s: String): String = buildString {
        for (c in s) {
            when (c) {
                '&' -> append("&amp;")
                '<' -> append("&lt;")
                '>' -> append("&gt;")
                '"' -> append("&quot;")
                '\'' -> append("&#39;")
                else -> append(c)
            }
        }
    }

    private fun targetUri(raw: String?): URI? =
        runCatching { URI(raw ?: "") }.getOrNull()

    private fun defaultPort(scheme: String, port: Int): Int =
        if (port == -1) if (scheme == "https") 443 else 80 else port
}
