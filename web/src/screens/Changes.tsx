// Changes.tsx — /sessions/{key}/changes: working-tree file list, one
// file's diff, per-line comment drafts in the device tray (the legacy
// trayKey format, so drafts survive), Send All through herdr.
import { Sheet, Toast } from "konsta/react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { ChangeFile, ChangesDiff, ChangesList, PendingComment } from "../api";
import { getChanges, getDiff, sendAllComments } from "../api";
import { FaXmark } from "../components/fa";
import { ScreenHeader } from "../components/ScreenHeader";
import { connectStream } from "../stream";

// Same key and shape the templ island used: drafts written there are
// still here after the rewrite.
// Legacy couch-pi-cmts fallback: the 2026-10-21 rename moved the
// prefix; comment drafts written before it must survive.
const trayKey = (key: string): string => `pi-couch-cmts:${key}`;
const trayRead = (key: string): string | null => {
	try {
		const v = localStorage.getItem(trayKey(key));
		if (v !== null) return v;
		const legacy = localStorage.getItem(`couch-pi-cmts:${key}`);
		if (legacy === null) return null;
		localStorage.setItem(trayKey(key), legacy);
		localStorage.removeItem(`couch-pi-cmts:${key}`);
		return legacy;
	} catch {
		return null;
	}
};

function loadTray(key: string): PendingComment[] {
	try {
		const raw = trayRead(key);
		return raw ? (JSON.parse(raw) as PendingComment[]) : [];
	} catch {
		return [];
	}
}

function saveTray(key: string, cs: PendingComment[]): void {
	try {
		localStorage.setItem(trayKey(key), JSON.stringify(cs));
	} catch {
		// storage blocked: drafts stay in memory for this view
	}
}

type DiffRow = { kind: "meta" | "hunk" | "add" | "del" | "ctx"; text: string; line?: number };

function parseDiff(diff: string): DiffRow[] {
	const rows: DiffRow[] = [];
	let b = 0; // line number on the + side
	for (const raw of diff.split("\n")) {
		if (raw.startsWith("@@")) {
			const m = raw.match(/\+(\d+)/);
			b = m ? Number.parseInt(m[1], 10) : 0;
			rows.push({ kind: "hunk", text: raw });
		} else if (raw.startsWith("+")) {
			rows.push({ kind: "add", text: raw.slice(1), line: b });
			b++;
		} else if (raw.startsWith("-")) {
			rows.push({ kind: "del", text: raw.slice(1) });
		} else if (raw.startsWith("\\")) {
			rows.push({ kind: "meta", text: raw }); // no newline marker consumes nothing
		} else if (
			raw.startsWith("diff ") ||
			raw.startsWith("index ") ||
			raw.startsWith("--- ") ||
			raw.startsWith("+++ ")
		) {
			rows.push({ kind: "meta", text: raw });
		} else {
			rows.push({ kind: "ctx", text: raw.replace(/^ /, ""), line: b });
			b++;
		}
	}
	return rows;
}

const LABEL_COLOR: Record<string, string> = {
	mod: "bg-warning/20 text-warning",
	new: "bg-success/20 text-success",
	del: "bg-error/20 text-error",
	ren: "bg-accent/20 text-accent",
};

export default function ChangesScreen({ sessionKey }: { sessionKey: string }) {
	const [list, setList] = useState<ChangesList | null>(null);
	const [error, setError] = useState("");
	const [openFile, setOpenFile] = useState<string | null>(null);
	const [diff, setDiff] = useState<ChangesDiff | null>(null);
	const [tray, setTray] = useState<PendingComment[]>(() => loadTray(sessionKey));
	const [sheet, setSheet] = useState<{ file: string; line: number } | null>(null);
	const [draft, setDraft] = useState("");
	const [sending, setSending] = useState(false);
	const [toast, setToast] = useState("");
	const toastTimer = useRef(0);

	const say = useCallback((msg: string) => {
		setToast(msg);
		window.clearTimeout(toastTimer.current);
		toastTimer.current = window.setTimeout(() => setToast(""), 2500);
	}, []);

	const refresh = useCallback(
		(file: string | null) => {
			void getChanges(sessionKey)
				.then((l) => {
					setList(l);
					setError("");
				})
				.catch((e: unknown) => setError(String(e)));
			if (file) {
				void getDiff(sessionKey, file)
					.then(setDiff)
					.catch(() => setDiff(null));
			}
		},
		[sessionKey],
	);

	useEffect(() => {
		refresh(null);
	}, [refresh]);

	// Live: a hash-gated reset means the tree moved; refresh the list
	// and any open diff.
	useEffect(() => {
		const stream = connectStream(`/api/stream/changes/${encodeURIComponent(sessionKey)}`, {
			onEvent: (name) => {
				if (name === "reset") refresh(openFile);
			},
		});
		return () => stream.close();
	}, [sessionKey, openFile, refresh]);

	const open = (f: ChangeFile) => {
		if (f.label === "new" || f.binary) return;
		setOpenFile(f.path);
		setDiff(null);
		refresh(f.path);
	};

	const saveDraft = () => {
		if (!sheet) return;
		const body = draft.trim();
		if (!body) return;
		const next = [...tray, { file: sheet.file, line: sheet.line, text: body }];
		setTray(next);
		saveTray(sessionKey, next);
		setSheet(null);
		setDraft("");
	};

	const discard = (i: number) => {
		const next = tray.filter((_, j) => j !== i);
		setTray(next);
		saveTray(sessionKey, next);
	};

	const sendAll = async () => {
		if (tray.length === 0 || sending) return;
		setSending(true);
		const r = await sendAllComments(sessionKey, tray);
		setSending(false);
		if (r.ok) {
			setTray([]);
			saveTray(sessionKey, []);
			say("Comments sent to the agent");
		} else if (r.error === "notdriven") {
			say("No driver for this session");
		} else {
			// The tray stays the source of truth on any failure.
			say("Send failed, comments kept");
		}
	};

	const rows = diff ? parseDiff(diff.diff) : [];
	const back = openFile
		? () => {
				setOpenFile(null);
				setDiff(null);
				refresh(null);
			}
		: undefined;

	return (
		<div
			className="mx-auto flex min-h-dvh max-w-3xl flex-col"
			style={{ paddingBottom: "var(--safe-bottom, 0px)", paddingTop: "64px" }}
		>
			<ScreenHeader
				title={openFile ?? "Changes"}
				subtitle={list?.repo ? list.branch || "no branch" : "not a git repository"}
				onBack={back ?? (() => history.back())}
			/>

			<main className="flex-1 px-3 pb-40">
				{error && <p className="mt-6 rounded-xl bg-base-200 p-4 text-sm text-error">{error}</p>}
				{!openFile && (
					<ul className="mt-2 flex flex-col gap-2">
						{(list?.files ?? []).map((f) => (
							<li key={f.path}>
								<button
									type="button"
									onClick={() => open(f)}
									className="flex w-full items-center gap-3 rounded-2xl bg-base-200 px-4 py-3 text-left active:scale-[0.99]"
								>
									<span
										className={`shrink-0 rounded-md px-1.5 py-0.5 text-[11px] font-semibold ${LABEL_COLOR[f.label] ?? "bg-base-300 text-muted"}`}
									>
										{f.label}
									</span>
									<span className="min-w-0 flex-1 truncate text-[14px]">{f.path}</span>
									<span className="shrink-0 text-xs text-muted">
										{f.binary ? "binary" : `+${f.added} −${f.deleted}`}
									</span>
								</button>
							</li>
						))}
						{list?.repo && list.files.length === 0 && (
							<p className="mt-6 px-1 text-sm text-muted">Working tree is clean.</p>
						)}
						{list && !list.repo && (
							<p className="mt-6 px-1 text-sm text-muted">
								This session does not run in a git repository.
							</p>
						)}
					</ul>
				)}

				{openFile && (
					<div className="mt-2 overflow-x-auto rounded-2xl bg-base-200 text-[12px] leading-5">
						{!diff && <p className="p-4 text-sm text-muted">Loading diff…</p>}
						{diff && rows.length === 0 && <p className="p-4 text-sm text-muted">Empty diff.</p>}
						{rows.map((r, i) => (
							// Diff rows are a positional, never-reordered parse of one
							// text; the index is their identity.
							<button
								// biome-ignore lint/suspicious/noArrayIndexKey: static parse order
								key={`${i}:${r.line ?? "x"}`}
								type="button"
								onClick={() => {
									if (r.line === undefined) return;
									setSheet({ file: openFile, line: r.line });
									setDraft("");
								}}
								className={`block w-full text-left font-mono whitespace-pre ${
									r.kind === "add"
										? "bg-success/10 text-success"
										: r.kind === "del"
											? "bg-error/10 text-error"
											: r.kind === "hunk"
												? "px-2 text-muted"
												: r.kind === "meta"
													? "px-2 text-muted/60"
													: "px-2"
								} ${r.line !== undefined ? "hover:bg-base-300" : "cursor-default"}`}
							>
								{r.line !== undefined && (
									<span className="mr-2 inline-block w-8 shrink-0 select-none text-right text-muted/50">
										{r.line}
									</span>
								)}
								{r.text}
							</button>
						))}
						{diff?.truncated && (
							<p className="px-3 py-2 text-xs text-warning">Diff truncated by the server.</p>
						)}
					</div>
				)}
			</main>

			{/* tray bar */}
			{tray.length > 0 && (
				<div className="fixed inset-x-0 bottom-0 z-20 mx-auto max-w-3xl border-t border-base-300 bg-base-300/95 px-3 pt-2 backdrop-blur">
					{tray.map((c, i) => (
						// Tray rows delete by index and may hold duplicates.
						<div
							// biome-ignore lint/suspicious/noArrayIndexKey: delete-by-index
							key={`${c.file}:${c.line}:${i}`}
							className="flex items-center gap-2 py-1 text-[13px]"
						>
							<span className="min-w-0 flex-1 truncate">
								<span className="text-muted">
									{c.file}:{c.line}
								</span>{" "}
								{c.text}
							</span>
							<button
								type="button"
								aria-label="Discard comment"
								onClick={() => discard(i)}
								className="shrink-0 rounded-lg px-2 py-1 text-muted active:scale-95"
							>
								<FaXmark size={14} />
							</button>
						</div>
					))}
					<div className="flex items-center gap-2 py-2">
						<span className="text-xs text-muted">{tray.length} pending</span>
						<button
							type="button"
							disabled={sending}
							onClick={() => void sendAll()}
							className="ml-auto min-h-[44px] rounded-xl bg-primary px-5 font-medium text-primary-content active:scale-[0.98] disabled:opacity-50"
						>
							{sending ? "Sending…" : "Send all"}
						</button>
					</div>
				</div>
			)}

			{/* comment draft sheet */}
			<Sheet opened={sheet !== null} onBackdropClick={() => setSheet(null)} className="pb-safe-all">
				<div className="p-4">
					<p className="pb-2 text-sm text-muted">{sheet ? `${sheet.file}:${sheet.line}` : ""}</p>
					<textarea
						value={draft}
						onChange={(e) => setDraft(e.target.value)}
						rows={3}
						placeholder="Comment for this line…"
						className="w-full rounded-xl bg-base-200 p-3 text-[15px]"
						autoCorrect="off"
					/>
					<div className="flex gap-2 pt-3">
						<button
							type="button"
							onClick={() => setSheet(null)}
							className="min-h-[44px] flex-1 rounded-xl bg-base-300 active:scale-[0.98]"
						>
							Cancel
						</button>
						<button
							type="button"
							onClick={saveDraft}
							className="min-h-[44px] flex-1 rounded-xl bg-primary font-medium text-primary-content active:scale-[0.98]"
						>
							Save
						</button>
					</div>
				</div>
			</Sheet>

			{toast && <Toast opened={true}>{toast}</Toast>}
		</div>
	);
}
