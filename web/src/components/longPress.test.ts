import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createLongPress, LONG_PRESS_MS, LONG_PRESS_SLOP_PX } from "./longPress";

const pt = (x: number, y: number) => ({ clientX: x, clientY: y });
const clickEvent = () => ({ preventDefault: vi.fn(), stopPropagation: vi.fn() });

describe("createLongPress", () => {
	beforeEach(() => vi.useFakeTimers());
	afterEach(() => vi.useRealTimers());

	it("fires exactly once after the hold", () => {
		const fire = vi.fn();
		const h = createLongPress(fire);
		h.onPointerDown(pt(0, 0));
		vi.advanceTimersByTime(LONG_PRESS_MS - 1);
		expect(fire).not.toHaveBeenCalled();
		vi.advanceTimersByTime(1);
		expect(fire).toHaveBeenCalledTimes(1);
	});

	it("does not fire when released early", () => {
		const fire = vi.fn();
		const h = createLongPress(fire);
		h.onPointerDown(pt(0, 0));
		h.onPointerUp();
		vi.advanceTimersByTime(LONG_PRESS_MS * 2);
		expect(fire).not.toHaveBeenCalled();
	});

	it("cancels on a move beyond the slop - the scroll guard", () => {
		const fire = vi.fn();
		const h = createLongPress(fire);
		h.onPointerDown(pt(0, 0));
		h.onPointerMove(pt(0, LONG_PRESS_SLOP_PX + 5));
		vi.advanceTimersByTime(LONG_PRESS_MS * 2);
		expect(fire).not.toHaveBeenCalled();
	});

	it("keeps the timer for moves inside the slop", () => {
		const fire = vi.fn();
		const h = createLongPress(fire);
		h.onPointerDown(pt(0, 0));
		h.onPointerMove(pt(3, 4));
		vi.advanceTimersByTime(LONG_PRESS_MS);
		expect(fire).toHaveBeenCalledTimes(1);
	});

	it("does not fire after pointercancel (scroll took over)", () => {
		const fire = vi.fn();
		const h = createLongPress(fire);
		h.onPointerDown(pt(0, 0));
		h.onPointerCancel();
		vi.advanceTimersByTime(LONG_PRESS_MS * 2);
		expect(fire).not.toHaveBeenCalled();
	});

	it("re-arms after a cancel", () => {
		const fire = vi.fn();
		const h = createLongPress(fire);
		h.onPointerDown(pt(0, 0));
		h.onPointerUp();
		h.onPointerDown(pt(0, 0));
		vi.advanceTimersByTime(LONG_PRESS_MS);
		expect(fire).toHaveBeenCalledTimes(1);
	});

	it("swallows the navigating click a fired hold ends in, once", () => {
		const fire = vi.fn();
		const h = createLongPress(fire);
		h.onPointerDown(pt(0, 0));
		vi.advanceTimersByTime(LONG_PRESS_MS);
		const first = clickEvent();
		expect(h.onClick(first)).toBe(true);
		expect(first.preventDefault).toHaveBeenCalledTimes(1);
		expect(first.stopPropagation).toHaveBeenCalledTimes(1);
		// The swallow is consume-once: a real tap afterwards navigates.
		const second = clickEvent();
		expect(h.onClick(second)).toBe(false);
		expect(second.preventDefault).not.toHaveBeenCalled();
	});

	it("lets a plain tap's click through untouched", () => {
		const fire = vi.fn();
		const h = createLongPress(fire);
		h.onPointerDown(pt(0, 0));
		h.onPointerUp();
		const e = clickEvent();
		expect(h.onClick(e)).toBe(false);
		expect(e.preventDefault).not.toHaveBeenCalled();
		expect(fire).not.toHaveBeenCalled();
	});
});
