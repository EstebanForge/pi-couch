// Native and web haptics bridge.
// When running in the Android APK, delegates directly to CouchPiHaptics
// (view.performHapticFeedback). In standard browsers, falls back to navigator.vibrate.

declare global {
	interface Window {
		CouchPiHaptics?: {
			tick: () => void;
			confirm: () => void;
			longPress: () => void;
		};
	}
}

export function hapticTick(): void {
	if (typeof window !== "undefined" && window.CouchPiHaptics?.tick) {
		window.CouchPiHaptics.tick();
		return;
	}
	if (typeof navigator !== "undefined" && typeof navigator.vibrate === "function") {
		navigator.vibrate(8);
	}
}

export function hapticConfirm(): void {
	if (typeof window !== "undefined" && window.CouchPiHaptics?.confirm) {
		window.CouchPiHaptics.confirm();
		return;
	}
	if (typeof navigator !== "undefined" && typeof navigator.vibrate === "function") {
		navigator.vibrate([12, 40, 12]);
	}
}

export function hapticLongPress(): void {
	if (typeof window !== "undefined" && window.CouchPiHaptics?.longPress) {
		window.CouchPiHaptics.longPress();
		return;
	}
	if (typeof navigator !== "undefined" && typeof navigator.vibrate === "function") {
		navigator.vibrate(25);
	}
}
