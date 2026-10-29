import { describe, expect, it, vi } from "vitest";
import { createStagedClose } from "./useStagedClose";

describe("createStagedClose", () => {
	it("flips the exit stage immediately, closes after the animation slot", () => {
		vi.useFakeTimers();
		try {
			const onStage = vi.fn();
			const onClose = vi.fn();
			const close = createStagedClose(onStage, onClose, 180).close;
			close();
			expect(onStage).toHaveBeenCalledTimes(1);
			expect(onClose).not.toHaveBeenCalled();
			vi.advanceTimersByTime(180);
			expect(onClose).toHaveBeenCalledTimes(1);
		} finally {
			vi.useRealTimers();
		}
	});

	it("ignores repeat closes while the exit animation runs", () => {
		vi.useFakeTimers();
		try {
			const onStage = vi.fn();
			const onClose = vi.fn();
			const close = createStagedClose(onStage, onClose, 180).close;
			close();
			close();
			close();
			vi.advanceTimersByTime(180);
			expect(onStage).toHaveBeenCalledTimes(1);
			expect(onClose).toHaveBeenCalledTimes(1);
		} finally {
			vi.useRealTimers();
		}
	});

	it("lets the owner cancel a pending close on unmount", () => {
		vi.useFakeTimers();
		try {
			const onStage = vi.fn();
			const onClose = vi.fn();
			const staged = createStagedClose(onStage, onClose, 180);
			staged.close();
			staged.cancel();
			vi.advanceTimersByTime(180);
			expect(onClose).not.toHaveBeenCalled();
			// cancel also re-arms nothing: a cancelled close stays closed
			staged.close();
			vi.advanceTimersByTime(180);
			expect(onClose).not.toHaveBeenCalled();
		} finally {
			vi.useRealTimers();
		}
	});
});
