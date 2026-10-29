package com.estebanforge.couchpi

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

// The WebView carries the mic bridge, so the navigation guard is the
// security boundary: only the configured origin loads in-app.
class NavGuardTest {

    private val origin = "https://zenless.tail-net.ts.net:8443"

    @Test
    fun `same origin stays in app`() {
        assertFalse(NavGuard.opensExternally(origin, origin))
        assertFalse(NavGuard.opensExternally(origin, "$origin/sessions"))
        assertFalse(NavGuard.opensExternally(origin, "$origin/api/stream/sessions"))
    }

    @Test
    fun `default and explicit ports are the same origin`() {
        assertFalse(NavGuard.opensExternally("https://zenless.tail-net.ts.net", "https://zenless.tail-net.ts.net:443/"))
        assertFalse(NavGuard.opensExternally("http://localhost:8090", "http://localhost:8090/x"))
    }

    @Test
    fun `foreign hosts and schemes open externally`() {
        assertTrue(NavGuard.opensExternally(origin, "https://evil.example.com/phish"))
        assertTrue(NavGuard.opensExternally(origin, "$origin.evil.example.com/"))
        assertTrue(NavGuard.opensExternally(origin, "http://zenless.tail-net.ts.net:8443/"))
        assertTrue(NavGuard.opensExternally(origin, "javascript:alert(1)"))
        assertTrue(NavGuard.opensExternally(origin, "file:///etc/hosts"))
        assertTrue(NavGuard.opensExternally(origin, "about:blank"))
    }

    @Test
    fun `junk input fails closed to the browser`() {
        assertTrue(NavGuard.opensExternally(null, "https://zenless.tail-net.ts.net:8443/"))
        assertTrue(NavGuard.opensExternally("", "https://zenless.tail-net.ts.net:8443/"))
        assertTrue(NavGuard.opensExternally(origin, null))
        assertTrue(NavGuard.opensExternally(origin, "not a url"))
        assertTrue(NavGuard.opensExternally(origin, "://no-scheme"))
    }

    @Test
    fun `host case differences still match`() {
        assertFalse(NavGuard.opensExternally("https://ZENLESS.Tail-Net.ts.net", "https://zenless.tail-net.ts.net/"))
    }

    @Test
    fun `attribute escape neutralizes quote injection`() {
        val hostile = "https://x/'-alert(1)-'\" onclick=\""
        val escaped = NavGuard.htmlAttrEscape(hostile)
        for (ch in listOf('\'', '"', '<', '>')) {
            assertFalse("raw ${ch} survived", escaped.contains(ch))
        }
        assertEquals(
            "https://x/&#39;-alert(1)-&#39;&quot; onclick=&quot;",
            escaped,
        )
        assertEquals("&amp;", NavGuard.htmlAttrEscape("&"))
    }
}
