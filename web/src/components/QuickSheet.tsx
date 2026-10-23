import { Sheet } from "konsta/react";
import { useEffect, useRef, useState } from "react";
import { createQuickAgent, getModels, type ModelRow, uploadImage } from "../api";
import { tildify } from "../fmt";
import { navigate, useSheetBack } from "../nav";
import { quickPath } from "../prefs";
import { FaCaretDown, FaPaperclip, FaXmark } from "./fa";
import { Spinner } from "./Spinner";

const THINKING_LEVELS = ["off", "minimal", "low", "medium", "high", "xhigh", "max"];
const MAX_FILES = 10;
const MAX_BYTES = 10 * 1024 * 1024;

type Pick = { id: number; name: string; path?: string; error?: string };

// The FAB speed dial's "Quick agent" step: a first prompt with
// attachments and an optional model/thinking pin, then the agent
// starts in the fixed Quick workspace at the configured path.
export function QuickSheet({ opened, onClose }: { opened: boolean; onClose: () => void }) {
	useSheetBack(opened, onClose);
	const [prompt, setPrompt] = useState("");
	const [files, setFiles] = useState<Pick[]>([]);
	const [models, setModels] = useState<ModelRow[] | null>(null);
	const [model, setModel] = useState("");
	const [thinking, setThinking] = useState("");
	const [pick, setPick] = useState<"none" | "model" | "thinking">("none");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");
	const fileInput = useRef<HTMLInputElement>(null);
	const nextId = useRef(1);

	useEffect(() => {
		if (!opened) return;
		setPrompt("");
		setFiles([]);
		setModel("");
		setThinking("");
		setPick("none");
		setBusy(false);
		setError("");
		getModels("")
			.then(setModels)
			.catch(() => setModels([]));
	}, [opened]);

	const uploading = files.some((f) => !f.path && !f.error);

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

	const create = () => {
		if (!prompt.trim() || uploading || busy) return;
		setBusy(true);
		setError("");
		// Attachment paths ride above the text, the same shape the
		// composer sends so pi reads the files off disk.
		const paths = files.filter((f) => f.path).map((f) => f.path as string);
		const body = paths.length > 0 ? [...paths, prompt].join("\n") : prompt;
		createQuickAgent("Quick", quickPath(), body, model || undefined, thinking || undefined)
			.then(({ key }) => {
				navigate(`/sessions/${encodeURIComponent(key)}`);
				onClose();
			})
			.catch((e: Error) => setError(e.message))
			.finally(() => setBusy(false));
	};

	const picker =
		pick === "model" ? (
			<div className="max-h-40 overflow-y-auto rounded-xl border border-muted/30">
				<button
					type="button"
					disabled={busy}
					onClick={() => {
						setModel("");
						setThinking("");
						setPick("none");
					}}
					className={`block w-full px-2 py-2 text-left text-[14px] ${model === "" ? "bg-primary/20 text-base-content" : ""}`}
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
						className={`block w-full truncate px-2 py-2 text-left text-[14px] ${model === m.id ? "bg-primary/20 text-base-content" : ""}`}
					>
						{m.id}
					</button>
				))}
			</div>
		) : pick === "thinking" ? (
			<div className="rounded-xl border border-muted/30">
				<button
					type="button"
					disabled={busy}
					onClick={() => {
						setThinking("");
						setPick("none");
					}}
					className={`block w-full px-2 py-2 text-left text-[14px] ${thinking === "" ? "bg-primary/20 text-base-content" : ""}`}
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
						className={`block w-full px-2 py-2 text-left text-[14px] ${thinking === lv ? "bg-primary/20 text-base-content" : ""}`}
					>
						{lv}
					</button>
				))}
			</div>
		) : null;

	return (
		<Sheet
			opened={opened}
			onBackdropClick={() => {
				if (!busy) onClose();
			}}
			className="pb-safe-all bg-base-200"
		>
			<div className="flex max-h-[80dvh] flex-col gap-2 p-3">
				<div className="text-[13px] font-semibold tracking-wider text-muted uppercase">
					Quick agent
				</div>

				<div className="truncate text-[12px] text-muted">{tildify(quickPath())}</div>

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

				{error && <div className="text-[12px] text-error">{error}</div>}

				<button
					type="button"
					disabled={busy || !prompt.trim() || uploading}
					onClick={create}
					className="flex min-h-[44px] items-center justify-center gap-2 rounded-xl bg-primary font-semibold text-primary-content disabled:opacity-50"
				>
					{busy && <Spinner size={14} />}
					{busy ? "Creating…" : "Start agent"}
				</button>
			</div>
		</Sheet>
	);
}
