import { useEffect, useRef, useState } from "react";

export const LONG_PRESS_MS = 500;
export const LONG_PRESS_SLOP_PX = 10;

type Pt = { clientX: number; clientY: number };
type ClickEvent = { preventDefault(): void; stopPropagation(): void };

export type LongPressHandlers = {
	onPointerDown: (e: Pt) => void;
	onPointerMove: (e: Pt) => void;
	onPointerUp: () => void;
	onPointerLeave: () => void;
	onPointerCancel: () => void;
	/** True when the click belonged to a fired hold and was swallowed;
	 * the caller navigates only when this is false. */
	onClick: (e: ClickEvent) => boolean;
};

// Hold-to-preview gesture core, framework-free so vitest can drive it
// with fake timers. The 10px move threshold is the real scroll guard;
// the timer only separates hold from tap. The navigating click a fired
// hold ends in is swallowed once, so the modal opens without the
// session opening under it.
export function createLongPress(fire: () => void): LongPressHandlers {
	let timer: ReturnType<typeof setTimeout> | null = null;
	let sx = 0;
	let sy = 0;
	let fired = false;
	const cancel = () => {
		if (timer !== null) {
			clearTimeout(timer);
			timer = null;
		}
	};
	return {
		onPointerDown(e) {
			sx = e.clientX;
			sy = e.clientY;
			fired = false;
			cancel();
			timer = setTimeout(() => {
				timer = null;
				fired = true;
				fire();
			}, LONG_PRESS_MS);
		},
		onPointerMove(e) {
			if (timer !== null && Math.hypot(e.clientX - sx, e.clientY - sy) > LONG_PRESS_SLOP_PX) {
				cancel();
			}
		},
		onPointerUp: cancel,
		onPointerLeave: cancel,
		onPointerCancel: cancel,
		onClick(e) {
			if (!fired) return false;
			fired = false;
			e.preventDefault();
			e.stopPropagation();
			return true;
		},
	};
}

export function useLongPress(fire: () => void): LongPressHandlers {
	const fireRef = useRef(fire);
	fireRef.current = fire;
	const [handlers] = useState(() => createLongPress(() => fireRef.current()));
	// A card unmounting mid-hold must not fire into a dead screen.
	useEffect(() => handlers.onPointerCancel, [handlers]);
	return handlers;
}
