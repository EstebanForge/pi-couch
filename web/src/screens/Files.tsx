// Files.tsx — /sessions/{key}/files: lazy per-directory listing of the
// session's repo plus a text/file preview.
import { Toast } from "konsta/react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { FilesList, FileView } from "../api";
import { getFiles, getFileView } from "../api";
import { FaFileLines, FaFolder } from "../components/fa";
import { ScreenHeader } from "../components/ScreenHeader";

const fmtSize = (n: number): string => {
	if (n < 1024) return `${n} B`;
	if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
	return `${(n / (1024 * 1024)).toFixed(1)} MB`;
};

export default function FilesScreen({ sessionKey }: { sessionKey: string }) {
	const [list, setList] = useState<FilesList | null>(null);
	const [dir, setDir] = useState("");
	const [view, setView] = useState<FileView | null>(null);
	const [error, setError] = useState("");
	const [toast, setToast] = useState("");
	const toastTimer = useRef(0);

	const say = (msg: string) => {
		setToast(msg);
		window.clearTimeout(toastTimer.current);
		toastTimer.current = window.setTimeout(() => setToast(""), 2500);
	};

	const loadDir = useCallback(
		(d: string) => {
			void getFiles(sessionKey, d)
				.then((l) => {
					setList(l);
					setDir(l.dir);
					setError("");
				})
				.catch((e: unknown) => setError(String(e)));
		},
		[sessionKey],
	);

	useEffect(() => {
		loadDir("");
	}, [loadDir]);

	const openFile = (name: string) => {
		const p = dir ? `${dir}/${name}` : name;
		void getFileView(sessionKey, p)
			.then(setView)
			.catch(() => say("Cannot read that file"));
	};

	const back = view
		? () => setView(null)
		: dir
			? () => loadDir(dir.includes("/") ? dir.slice(0, dir.lastIndexOf("/")) : "")
			: undefined;

	const segs = dir ? dir.split("/") : [];
	const jump = (i: number) => loadDir(segs.slice(0, i + 1).join("/"));

	const dirsFirst = (l: FilesList) =>
		[...l.entries].sort((a, b) =>
			a.dir === b.dir ? a.name.localeCompare(b.name) : a.dir ? -1 : 1,
		);

	return (
		<div
			className="mx-auto flex min-h-dvh max-w-3xl flex-col"
			style={{ paddingBottom: "var(--safe-bottom, 0px)", paddingTop: "64px" }}
		>
			<ScreenHeader
				title={
					view
						? (view.file.split("/").pop() ?? "File")
						: segs.length
							? segs[segs.length - 1]
							: "Files"
				}
				subtitle={list?.repo ? dir || "/" : "not a git repository"}
				onBack={back ?? (() => history.back())}
			/>

			<main className="flex-1 px-3 pb-24">
				{error && <p className="mt-6 rounded-xl bg-base-200 p-4 text-sm text-error">{error}</p>}

				{!view && list?.repo && (
					<>
						{segs.length > 0 && (
							<div className="flex flex-wrap items-center gap-1 px-1 py-2 text-[12px] text-muted">
								<button
									type="button"
									onClick={() => loadDir("")}
									className="rounded-md px-1 py-0.5 active:scale-95"
								>
									root
								</button>
								{segs.map((s, i) => (
									// Breadcrumb segments are positional by nature.
									<span
										// biome-ignore lint/suspicious/noArrayIndexKey: positional
										key={i}
										className="flex items-center gap-1"
									>
										<span>/</span>
										{i < segs.length - 1 ? (
											<button
												type="button"
												onClick={() => jump(i)}
												className="rounded-md px-1 py-0.5 active:scale-95"
											>
												{s}
											</button>
										) : (
											<span className="text-base-content">{s}</span>
										)}
									</span>
								))}
							</div>
						)}
						<ul className="mt-1 flex flex-col gap-1.5">
							{dirsFirst(list).map((e) => (
								<li key={e.name}>
									<button
										type="button"
										onClick={() =>
											e.dir ? loadDir(dir ? `${dir}/${e.name}` : e.name) : openFile(e.name)
										}
										className="flex w-full items-center gap-3 rounded-xl bg-base-200 px-4 py-2.5 text-left active:scale-[0.99]"
									>
										<span className="shrink-0 text-muted">
											{e.dir ? (
												<FaFolder size={14} />
											) : (
												<FaFileLines size={13} className="opacity-60" />
											)}
										</span>
										<span className="min-w-0 flex-1 truncate text-[14px]">{e.name}</span>
										{!e.dir && (
											<span className="shrink-0 text-xs text-muted">{fmtSize(e.size)}</span>
										)}
									</button>
								</li>
							))}
						</ul>
						{list.entries.length === 0 && (
							<p className="mt-6 px-1 text-sm text-muted">Empty directory.</p>
						)}
					</>
				)}
				{!view && list && !list.repo && (
					<p className="mt-6 px-1 text-sm text-muted">
						This session does not run in a git repository.
					</p>
				)}

				{view && (
					<div className="mt-2 overflow-x-auto rounded-2xl bg-base-200">
						<pre className="p-3 font-mono text-[12px] leading-5 whitespace-pre">{view.content}</pre>
					</div>
				)}
			</main>

			{toast && <Toast opened={true}>{toast}</Toast>}
		</div>
	);
}
