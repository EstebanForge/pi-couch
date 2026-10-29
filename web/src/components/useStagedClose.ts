import { useCallback, useEffect, useRef, useState } from "react";

// Plays the exit animation before the parent unmounts the modal: the
// first close flips the closing stage (swapping animation classes) and
// the real onClose fires once the out-animation had its slot. cancel()
// is for the owner unmounting mid-exit, so onClose never fires into a
// dead tree.
export function createStagedClose(
	onStage: () => void,
	onClose: () => void,
	ms: number,
): { close: () => void; cancel: () => void } {
	let timer: ReturnType<typeof setTimeout> | null = null;
	let fired = false;
	return {
		close() {
			if (fired) return;
			fired = true;
			onStage();
			timer = setTimeout(onClose, ms);
		},
		cancel() {
			if (timer !== null) {
				clearTimeout(timer);
				timer = null;
			}
		},
	};
}

export function useStagedClose(onClose: () => void, ms = 180) {
	const [closing, setClosing] = useState(false);
	const onCloseRef = useRef(onClose);
	onCloseRef.current = onClose;
	const stagedRef = useRef<ReturnType<typeof createStagedClose> | null>(null);
	stagedRef.current ??= createStagedClose(
		() => setClosing(true),
		() => onCloseRef.current(),
		ms,
	);
	useEffect(() => () => stagedRef.current?.cancel(), []);
	const close = useCallback(() => stagedRef.current?.close(), []);
	return { closing, close };
}
