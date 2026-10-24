import { useEffect, useRef, useState } from "react";
import { getModels, type ModelRow, uploadImage } from "../api";
import { FaCaretDown, FaPaperclip, FaXmark } from "./fa";
import { Spinner } from "./Spinner";

const THINKING_LEVELS = ["off", "minimal", "low", "medium", "high", "xhigh", "max"];
const MAX_FILES = 10;
const MAX_BYTES = 10 * 1024 * 1024;

type Pick = { id: number; name: string; path?: string; error?: string };

export type LaunchFields = { body: string; model?: string; thinking?: string; uploading: boolean };

// The prompt/attachments/model/thinking block shared by the quick
// and new-agent create sheets. Owns its upload and catalog state and
// reports the composed send body (attachment paths ride above the
// text, the shape the composer sends so pi reads the files off disk).
export function AgentLauncherFields({
	opened,
	busy,
	onChange,
}: {
	opened: boolean;
	busy: boolean;
	onChange: (v: LaunchFields) => void;
}) {
	const [prompt, setPrompt] = useState("");
	const [files, setFiles] = useState<Pick[]>([]);
	const [models, setModels] = useState<ModelRow[] | null>(null);
	const [model, setModel] = useState("");
	const [thinking, setThinking] = useState("");
	const [pick, setPick] = useState<"none" | "model" | "thinking">("none");
	const fileInput = useRef<HTMLInputElement>(null);
	const nextId = useRef(1);

	useEffect(() => {
		getModels("")
			.then(setModels)
			.catch(() => setModels([]));
	}, []);

	// A reopened sheet starts a fresh draft.
	useEffect(() => {
		if (!opened) return;
		setPrompt("");
		setFiles([]);
		setModel("");
		setThinking("");
		setPick("none");
	}, [opened]);

	const uploading = files.some((f) => !f.path && !f.error);

	// Hold the caller's callback in a ref so the report effect can
	// depend on state only — a new parent closure per render must not
	// re-fire the report (and loop through the parent's setState).
	const report = useRef(onChange);
	report.current = onChange;
	useEffect(() => {
		const paths = files.filter((f) => f.path).map((f) => f.path as string);
		report.current({
			body: paths.length > 0 ? [...paths, prompt].join("\n") : prompt,
			model: model || undefined,
			thinking: thinking || undefined,
			uploading,
		});
	}, [prompt, files, model, thinking, uploading]);

	const onFiles = (list: FileList | null) => {
		if (!list) return;
		for (const file of list) {
			if (files.length >= MAX_FILES) break;
			const id = nextId.current++;
			setFiles((cur) => [...cur, { id, name: file.name }]);
			if (file.size > MAX_BYTES) {
				setFiles((cur) => cur.map((f) => (f.id === id ? { ...f, error: "over 10 MB" } : f)));
				continue;
			}
			uploadImage(file)
				.then((path) => setFiles((cur) => cur.map((f) => (f.id === id ? { ...f, path } : f))))
				.catch(() =>
					setFiles((cur) => cur.map((f) => (f.id === id ? { ...f, error: "upload failed" } : f))),
				);
		}
	};

	const picker =
		pick === "model" ? (
			<div className="flex max-h-48 flex-col gap-1 overflow-y-auto rounded-2xl border border-base-content/10 bg-base-300/60 p-1.5 shadow-surface-sm backdrop-blur-sm">
				<button
					type="button"
					disabled={busy}
					onClick={() => {
						setModel("");
						setThinking("");
						setPick("none");
					}}
					className={`press-scale block w-full rounded-xl px-3 py-2 text-left text-[14px] transition-all ${
						model === ""
							? "border border-primary/40 bg-primary/20 font-medium text-base-content"
							: "text-base-content/80 active:bg-base-200"
					}`}
				>
					Agent default
				</button>
				{(models ?? []).map((m) => (
					<button
						key={m.id}
						type="button"
						disabled={busy}
						onClick={() => {
							setModel(m.id);
							setPick("none");
						}}
						className={`press-scale block w-full truncate rounded-xl px-3 py-2 text-left font-mono text-[13.5px] transition-all ${
							model === m.id
								? "border border-primary/40 bg-primary/20 font-medium text-base-content"
								: "text-base-content/80 active:bg-base-200"
						}`}
					>
						{m.id}
					</button>
				))}
			</div>
		) : pick === "thinking" ? (
			<div className="flex flex-col gap-1 rounded-2xl border border-base-content/10 bg-base-300/60 p-1.5 shadow-surface-sm backdrop-blur-sm">
				<button
					type="button"
					disabled={busy}
					onClick={() => {
						setThinking("");
						setPick("none");
					}}
					className={`press-scale block w-full rounded-xl px-3 py-2 text-left text-[14px] transition-all ${
						thinking === ""
							? "border border-primary/40 bg-primary/20 font-medium text-base-content"
							: "text-base-content/80 active:bg-base-200"
					}`}
				>
					Model default
				</button>
				{THINKING_LEVELS.map((lv) => (
					<button
						key={lv}
						type="button"
						disabled={busy}
						onClick={() => {
							setThinking(lv);
							setPick("none");
						}}
						className={`press-scale block w-full rounded-xl px-3 py-2 text-left text-[14px] capitalize transition-all ${
							thinking === lv
								? "border border-primary/40 bg-primary/20 font-medium text-base-content"
								: "text-base-content/80 active:bg-base-200"
						}`}
					>
						{lv}
					</button>
				))}
			</div>
		) : null;

	return (
		<>
			<textarea
				value={prompt}
				onChange={(e) => setPrompt(e.target.value)}
				rows={3}
				placeholder="First prompt for the agent…"
				className="rounded-xl border border-muted/40 bg-base-300 px-3 py-2 text-[16px] outline-none focus:border-primary/60"
			/>

			<div className="flex items-center gap-2">
				<button
					type="button"
					aria-label="Attach files"
					disabled={files.length >= MAX_FILES}
					onClick={() => fileInput.current?.click()}
					className="relative flex h-9 w-9 items-center justify-center rounded-lg bg-base-300 text-muted disabled:opacity-40"
				>
					<FaPaperclip size={16} />
					{files.length > 0 && (
						<span className="absolute -top-1 -right-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold text-primary-content">
							{files.length}
						</span>
					)}
				</button>
				<input
					ref={fileInput}
					type="file"
					accept="image/*"
					multiple
					className="hidden"
					onChange={(e) => {
						onFiles(e.target.files);
						e.target.value = "";
					}}
				/>

				<button
					type="button"
					disabled={busy}
					onClick={() => setPick(pick === "model" ? "none" : "model")}
					className="flex min-h-9 flex-1 items-center gap-1.5 rounded-lg bg-base-300 px-2.5 text-[13px]"
				>
					<span className="truncate">{model || "Agent default"}</span>
					<FaCaretDown size={11} className="ml-auto shrink-0 text-muted" />
				</button>
				<button
					type="button"
					disabled={busy || !model}
					onClick={() => setPick(pick === "thinking" ? "none" : "thinking")}
					className="flex min-h-9 items-center gap-1.5 rounded-lg bg-base-300 px-2.5 text-[13px] disabled:opacity-40"
				>
					<span className="truncate">{thinking || "Thinking"}</span>
					<FaCaretDown size={11} className="shrink-0 text-muted" />
				</button>
			</div>

			{pick === "thinking" && !model && (
				<div className="text-[11px] text-muted">
					Pick a model first — thinking rides the model flag.
				</div>
			)}
			{picker}

			{files.map((f) => (
				<div key={f.id} className="flex items-center gap-2 text-[12px]">
					<span className="min-w-0 flex-1 truncate text-muted">
						{f.name}
						{f.path && <span className="text-success"> ✓</span>}
						{f.error && <span className="text-error"> · {f.error}</span>}
					</span>
					<button
						type="button"
						aria-label={`Remove ${f.name}`}
						disabled={busy}
						onClick={() => setFiles((cur) => cur.filter((x) => x.id !== f.id))}
						className="text-muted disabled:opacity-40"
					>
						<FaXmark size={12} />
					</button>
				</div>
			))}

			{uploading && (
				<div className="flex items-center gap-2 text-[12px] text-muted">
					<Spinner size={12} /> uploading…
				</div>
			)}
		</>
	);
}
