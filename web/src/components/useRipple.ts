import { TouchRipple } from "konsta/shared/utils";
import { type RefObject, useEffect, useRef } from "react";
import { hapticTick } from "../haptics";

/**
 * Attaches Konsta Material Touch Ripple to a DOM element.
 * Works seamlessly across desktop clicks and mobile touch events.
 */
export function useTouchRipple(
	elRef: RefObject<HTMLElement | null>,
	enabled = true,
	eventsElRef?: RefObject<HTMLElement | null>,
) {
	const ripple = useRef<TouchRipple | null>(null);

	useEffect(() => {
		if (!enabled) return;
		const target = (eventsElRef ?? elRef).current;
		const container = elRef.current;
		if (!target || !container) return;

		const removeRipple = () => {
			if (ripple.current) {
				ripple.current.remove();
				ripple.current = null;
			}
		};

		const onPointerDown = (e: PointerEvent) => {
			// Only left-click or touch triggers ripple
			if (e.button !== 0 && e.pointerType === "mouse") return;
			hapticTick();
			ripple.current = new TouchRipple(container, e.pageX, e.pageY);
		};

		target.addEventListener("pointerdown", onPointerDown, { passive: true });
		target.addEventListener("pointermove", removeRipple, { passive: true });
		target.addEventListener("pointerup", removeRipple, { passive: true });
		target.addEventListener("pointercancel", removeRipple, { passive: true });
		target.addEventListener("contextmenu", removeRipple, { passive: true });

		return () => {
			target.removeEventListener("pointerdown", onPointerDown);
			target.removeEventListener("pointermove", removeRipple);
			target.removeEventListener("pointerup", removeRipple);
			target.removeEventListener("pointercancel", removeRipple);
			target.removeEventListener("contextmenu", removeRipple);
			removeRipple();
		};
	}, [elRef, eventsElRef, enabled]);
}
