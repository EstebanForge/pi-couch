// Web Speech dictation: browser-provided speech recognition, no key,
// no server round trip. Chromium ships it; the Android WebView may not,
// so callers feature-detect and hide the control. Finals commit into
// the draft; interim results are deliberately ignored (a controlled
// textarea plus a live hypothesis fights the draft state for no gain).
//
// A mic-less engine can start() and then never fire anything - no
// result, no error, no end (observed in headless Chromium) - so a
// watchdog turns silence into the same failure path as an error.

type RecognitionLike = {
	lang: string;
	continuous: boolean;
	interimResults: boolean;
	start: () => void;
	stop: () => void;
	onresult: ((e: SpeechEventLike) => void) | null;
	onerror: ((e: { error?: string }) => void) | null;
	onend: (() => void) | null;
};
type RecognitionCtor = new () => RecognitionLike;
type SpeechEventLike = {
	resultIndex: number;
	results: ArrayLike<{ isFinal: boolean; 0: { transcript: string } }>;
};

function ctors(): [RecognitionCtor?, RecognitionCtor?] {
	if (typeof window === "undefined") return [];
	const w = window as unknown as {
		SpeechRecognition?: RecognitionCtor;
		webkitSpeechRecognition?: RecognitionCtor;
	};
	return [w.SpeechRecognition, w.webkitSpeechRecognition];
}

export function dictationSupported(): boolean {
	const [a, b] = ctors();
	return !!(a || b);
}

export type Dictation = { stop: () => void };

const SILENCE_MS = 15_000; // long enough for a permission prompt

export function startDictation(cb: {
	onFinal: (text: string) => void;
	onEnd: () => void;
	onError: () => void;
}): Dictation | null {
	const [a, b] = ctors();
	const Ctor = a ?? b;
	if (!Ctor) return null;
	const rec = new Ctor();
	rec.lang = typeof navigator !== "undefined" && navigator.language ? navigator.language : "en";
	rec.continuous = true;
	rec.interimResults = true;
	let alive = true;
	let heardAnything = false;
	const finish = () => {
		alive = false;
		clearTimeout(watchdog);
		rec.onresult = rec.onerror = rec.onend = null;
	};
	const watchdog = setTimeout(() => {
		if (!alive) return;
		if (heardAnything) return; // engine alive, just quiet
		finish();
		try {
			rec.stop();
		} catch {
			// already dead
		}
		cb.onError();
	}, SILENCE_MS);
	rec.onresult = (e) => {
		for (let i = e.resultIndex; i < e.results.length; i++) {
			const r = e.results[i];
			// Empty transcripts (some engines emit periodic empty interim
			// results with no microphone) are not activity: the silence
			// watchdog must keep counting.
			if (r[0].transcript.trim()) {
				heardAnything = true;
				if (r.isFinal) cb.onFinal(r[0].transcript.trim());
			}
		}
	};
	rec.onerror = () => {
		if (!alive) return;
		finish();
		cb.onError();
	};
	rec.onend = () => {
		if (!alive) return;
		finish();
		cb.onEnd();
	};
	try {
		rec.start();
	} catch {
		clearTimeout(watchdog);
		cb.onError();
		return null;
	}
	return {
		stop: () => {
			finish(); // an explicit stop is not an error and not an end
			try {
				rec.stop();
			} catch {
				// already stopped
			}
		},
	};
}
