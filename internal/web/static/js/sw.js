// Couch Pi push service worker (root scope). Shows transition notifications
// and deep-links taps back into the session chat. No offline shell, no
// caching: the APK owns the install surface, this file only receives pushes.
self.addEventListener("push", (event) => {
	let data = {};
	try {
		data = event.data ? event.data.json() : {};
	} catch {
		data = { title: "Couch Pi", body: event.data ? event.data.text() : "" };
	}
	const title = data.title || "Couch Pi";
	const url = data.url || "/";
	event.waitUntil(
		self.registration.showNotification(title, {
			body: data.body || "",
			tag: url,
			renotify: true,
			data: { url },
		}),
	);
});

self.addEventListener("notificationclick", (event) => {
	event.notification.close();
	const url = (event.notification.data && event.notification.data.url) || "/";
	event.waitUntil(
		self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((clients) => {
			for (const client of clients) {
				const path = new URL(client.url).pathname;
				if (path === url) {
					return client.focus();
				}
			}
			// Tapped while the tailnet is down: the browser shows the offline
			// retry screen from the failed navigation itself.
			return self.clients.openWindow(url);
		}),
	);
});

// Rotate the subscription when the push service invalidates it: resubscribe
// against the same VAPID key and mirror it to the server, best effort.
self.addEventListener("pushsubscriptionchange", (event) => {
	event.waitUntil(
		self.registration.pushManager
			.subscribe(event.oldSubscription ? event.oldSubscription.options : undefined)
			.then((sub) =>
				fetch("/api/push/subscribe", {
					method: "POST",
					headers: { "content-type": "application/json" },
					body: JSON.stringify(sub.toJSON()),
				}),
			)
			.catch(() => {}),
	);
});
