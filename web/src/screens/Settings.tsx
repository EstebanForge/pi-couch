// Settings.tsx — /settings: per-device UI prefs. Reached from the home
// side menu. Values live in localStorage (the settings screen writes,
// the composer reads at mount), so they follow the browser/device, not
// the session. Konsta supplies the controls (Segmented, Toggle); List
// stays rejected like everywhere else (material look fights dracula).
import { Segmented, SegmentedButton, Sheet, Toggle } from "konsta/react";
import { useEffect, useState } from "react";
import { getVersion } from "../api";
import { orderKeys, QUICK_KEYS } from "../chat/quick-keys";
import { DirBrowser } from "../components/DirBrowser";
import { FaArrowDown, FaArrowUp, FaCheck } from "../components/fa";
import { ScreenHeader } from "../components/ScreenHeader";
import { useSheetBack } from "../nav";
import {
	type EnterSendsPref,
	effectiveEnterSends,
	enterSendsPref,
	quickKeyOrder,
	quickKeysEnabled,
	quickPath,
	setEnterSendsPref,
	setQuickKeyOrder,
	setQuickKeysEnabled,
	setQuickPath,
} from "../prefs";
import { activeScheme, SCHEMES, setScheme } from "../theme";

const ENTER_CHOICES: { value: EnterSendsPref; label: string; hint: string }[] = [
	{ value: "auto", label: "Auto", hint: "send on a keyboard, newline on touch" },
	{ value: "send", label: "Always send", hint: "Enter always sends" },
	{ value: "newline", label: "Always newline", hint: "Enter always adds a line" },
];

export default function SettingsScreen() {
	const [schemeId, setSchemeId] = useState(activeScheme().id);
	const [enter, setEnter] = useState<EnterSendsPref>(enterSendsPref);
	const [keys, setKeys] = useState(quickKeysEnabled);
	const [order, setOrder] = useState(() => orderKeys(quickKeyOrder()).map((k) => k.id));
	const [qPath, setQPath] = useState(quickPath);
	const [pickerOpen, setPickerOpen] = useState(false);
	useSheetBack(pickerOpen, () => setPickerOpen(false));
	const [picked, setPicked] = useState("");
	const [version, setVersion] = useState<string | null>(null);

	useEffect(() => {
		getVersion()
			.then(setVersion)
			.catch(() => {});
	}, []);

	// move swaps one key past its neighbor and persists the whole id
	// list; the strip re-reads it at mount.
	const move = (id: string, dir: -1 | 1) => {
		const i = order.indexOf(id);
		const j = i + dir;
		if (i < 0 || j < 0 || j >= order.length) return;
		const next = [...order];
		[next[i], next[j]] = [next[j], next[i]];
		setOrder(next);
		setQuickKeyOrder(next);
	};

	return (
		<div className="min-h-dvh bg-base-100 text-base-content">
			<ScreenHeader title="Settings" onBack={() => history.back()} />
			<div
				className="mx-auto flex min-h-dvh max-w-3xl flex-col gap-6 px-3 pt-[80px]"
				style={{ paddingBottom: "var(--safe-bottom, 0px)" }}
			>
				<section>
					<h2 className="px-1 pb-2 text-[11px] font-semibold uppercase tracking-wider text-muted">
						Color scheme
					</h2>
					<div className="overflow-hidden rounded-2xl border border-base-content/10 bg-base-200/80 shadow-surface-sm backdrop-blur-sm">
						{SCHEMES.map((s, i) => (
							<button
								key={s.id}
								type="button"
								onClick={() => {
									setScheme(s.id);
									setSchemeId(s.id);
								}}
								className={`press-scale flex w-full items-center gap-3 px-4 py-3.5 text-left active:bg-base-300 ${i > 0 ? "border-t border-base-content/8" : ""}`}
							>
								<span className="flex shrink-0 gap-1.5">
									{[
										s.vars["--color-base-100"],
										s.vars["--color-primary"],
										s.vars["--color-accent"],
										s.vars["--color-warning"],
									].map((c) => (
										<span
											key={c}
											className="h-4 w-4 rounded-full border border-white/10 shadow-sm"
											style={{ background: c }}
										/>
									))}
								</span>
								<span className="flex-1 text-[15.5px] font-medium text-base-content">
									{s.label}
								</span>
								{schemeId === s.id && <FaCheck size={15} className="text-primary" />}
							</button>
						))}
					</div>
					<p className="px-1 pt-2 text-xs text-muted">
						Applies everywhere at once, including the terminal grid.
					</p>
				</section>

				<section>
					<h2 className="px-1 pb-2 text-[11px] font-semibold uppercase tracking-wider text-muted">
						Enter key
					</h2>
					<div className="rounded-2xl border border-base-content/10 bg-base-200/80 p-1.5 shadow-surface-sm">
						<Segmented
							className="w-full"
							colors={{ strongBgMaterial: "bg-base-300", strongHighlightBgMaterial: "bg-primary" }}
						>
							{ENTER_CHOICES.map((c) => (
								<SegmentedButton
									key={c.value}
									active={enter === c.value}
									onClick={() => {
										setEnterSendsPref(c.value);
										setEnter(c.value);
									}}
									className={`text-[13px] font-medium ${enter === c.value ? "text-primary-content" : "text-base-content"}`}
								>
									{c.label}
								</SegmentedButton>
							))}
						</Segmented>
					</div>
					<p className="px-1 pt-2 text-xs text-muted">
						{ENTER_CHOICES.find((c) => c.value === enter)?.hint}
					</p>
					<p className="px-1 pt-1 text-xs text-muted">
						Current effective behavior: Enter {effectiveEnterSends() ? "sends" : "adds a newline"}.
					</p>
				</section>

				<section>
					<h2 className="px-1 pb-2 text-[11px] font-semibold uppercase tracking-wider text-muted">
						Quick keys
					</h2>
					<div className="flex items-center justify-between rounded-2xl border border-base-content/10 bg-base-200/80 px-4 py-3.5 shadow-surface-sm backdrop-blur-sm">
						<span className="text-[15px] font-medium text-base-content">
							Show the quick-keys strip in sessions
						</span>
						<Toggle
							checked={keys}
							onChange={() => {
								setQuickKeysEnabled(!keys);
								setKeys(!keys);
							}}
							colors={{ checkedBgMaterial: "bg-primary" }}
						/>
					</div>
					<p className="px-1 pt-2 text-xs text-muted">
						Esc, Enter, arrows and the dialog keys above the composer.
					</p>
					<div className="mt-3 overflow-hidden rounded-2xl border border-base-content/10 bg-base-200/80 shadow-surface-sm backdrop-blur-sm">
						{order.map((id, i) => (
							<div
								key={id}
								className={`flex items-center justify-between px-4 py-2 ${i > 0 ? "border-t border-base-content/8" : ""}`}
							>
								<span className="text-[15px] font-medium text-base-content">
									{QUICK_KEYS.find((k) => k.id === id)?.label ?? id}
								</span>
								<span className="flex gap-1.5">
									<button
										type="button"
										aria-label={`Move ${id} up`}
										disabled={i === 0}
										onClick={() => move(id, -1)}
										className="press-scale min-h-[38px] min-w-[44px] rounded-xl border border-base-content/8 bg-base-300/80 px-3 text-base-content active:bg-base-300 disabled:opacity-30"
									>
										<FaArrowUp size={14} />
									</button>
									<button
										type="button"
										aria-label={`Move ${id} down`}
										disabled={i === order.length - 1}
										onClick={() => move(id, 1)}
										className="press-scale min-h-[38px] min-w-[44px] rounded-xl border border-base-content/8 bg-base-300/80 px-3 text-base-content active:bg-base-300 disabled:opacity-30"
									>
										<FaArrowDown size={14} />
									</button>
								</span>
							</div>
						))}
					</div>
					<p className="px-1 pt-2 text-xs text-muted">
						Order the keys with the arrows; sessions pick it up on the next visit.
					</p>
				</section>

				<section>
					<h2 className="px-1 pb-2 text-[11px] font-semibold uppercase tracking-wider text-muted">
						Quick agent
					</h2>
					<button
						type="button"
						onClick={() => {
							setPicked(qPath);
							setPickerOpen(true);
						}}
						className="press-scale flex min-h-[50px] w-full items-center justify-between rounded-2xl border border-base-content/10 bg-base-200/80 px-4 text-left shadow-surface-sm backdrop-blur-sm active:bg-base-300"
					>
						<span className="truncate text-[15.5px] font-medium text-base-content">
							{qPath ? qPath.replace(/^\/home\/[^/]+/, "~") : "Choose a folder…"}
						</span>
						<span className="ml-2 shrink-0 rounded-lg bg-base-300/80 px-2.5 py-1 text-[11.5px] font-medium text-primary">
							browse
						</span>
					</button>
					<p className="px-1 pt-2 text-xs text-muted">
						The home FAB's Quick agent starts pi here without asking. Each tap adds one agent to the
						Quick workspace at this folder.
					</p>
				</section>

				<p className="pt-2 text-center text-xs text-muted">Pi Couch {version ?? "…"}</p>
			</div>

			<Sheet
				opened={pickerOpen}
				onBackdropClick={() => setPickerOpen(false)}
				className="pb-safe-all bg-base-200"
			>
				<div className="flex max-h-[80dvh] flex-col gap-2 p-3">
					<div className="text-[13px] font-semibold tracking-wider text-muted uppercase">
						Quick agent folder
					</div>
					<DirBrowser onPathChange={setPicked} />
					<button
						type="button"
						disabled={!picked}
						onClick={() => {
							setQPath(picked);
							setQuickPath(picked);
							setPickerOpen(false);
						}}
						className="press-scale flex min-h-[48px] items-center justify-center rounded-2xl bg-primary text-[15px] font-semibold text-primary-content shadow-[0_2px_12px_rgba(64,157,212,0.4)] disabled:opacity-50"
					>
						Use this folder
					</button>
				</div>
			</Sheet>
		</div>
	);
}
