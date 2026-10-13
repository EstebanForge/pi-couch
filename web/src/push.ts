// Push toggle for the home header bell. Port of the legacy initPush
// island (main.ts): permission is asked only on the explicit tap, the
// service worker registers on demand, and a browser without push
// support hides the button entirely. The worker itself stays the Go
// server's /sw.js until cutover moves it into this embed.

function urlBase64ToBytes(b64: string): Uint8Array<ArrayBuffer> {
	const pad = "=".repeat((4 - (b64.length % 4)) % 4);
	const raw = atob((b64 + pad).replace(/-/g, "+").replace(/_/g, "/"));
	const buf = new ArrayBuffer(raw.length);
	const out = new Uint8Array(buf);
	for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
	return out;
}

export type PushState = "unsupported" | "off" | "on";

export async function pushState(): Promise<PushState> {
	if (!("serviceWorker" in navigator) || !("PushManager" in window)) return "unsupported";
	try {
		const reg = await navigator.serviceWorker.getRegistration();
		const sub = await reg?.pushManager.getSubscription();
		return sub ? "on" : "off";
	} catch {
		// Some engines (headless Chrome) reject getSubscription when no
		// push service exists; treat as no-subscription, not an error.
		return "off";
	}
}

export async function togglePush(state: PushState): Promise<PushState> {
	if (state === "unsupported") return state;
	const reg = await navigator.serviceWorker.register("/sw.js");
	if (state === "on") {
		const sub = await reg.pushManager.getSubscription();
		if (sub) {
			await fetch("/api/push/unsubscribe", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(sub.toJSON()),
			});
			await sub.unsubscribe();
		}
		return "off";
	}
	if (Notification.permission === "denied") return "off";
	const ask = await Notification.requestPermission();
	if (ask !== "granted") return "off";
	const res = await fetch("/api/push/key");
	if (!res.ok) return "off";
	const { key } = (await res.json()) as { key: string };
	const sub = await reg.pushManager.subscribe({
		userVisibleOnly: true,
		applicationServerKey: urlBase64ToBytes(key),
	});
	const post = await fetch("/api/push/subscribe", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(sub.toJSON()),
	});
	if (!post.ok) {
		await sub.unsubscribe();
		return "off";
	}
	return "on";
}
