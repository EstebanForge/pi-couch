// Settings.tsx — /settings: per-device UI prefs. Reached from the home
// side menu. Values live in localStorage (the settings screen writes,
// the composer reads at mount), so they follow the browser/device, not
// the session. Konsta supplies the controls (Segmented, Toggle); List
// stays rejected like everywhere else (material look fights dracula).
import { Navbar, NavbarBackLink, Segmented, SegmentedButton, Toggle } from "konsta/react";
import { useEffect, useState } from "react";
import { getVersion } from "../api";
import { orderKeys, QUICK_KEYS } from "../chat/quick-keys";
import {
	type EnterSendsPref,
	effectiveEnterSends,
	enterSendsPref,
	quickKeyOrder,
	quickKeysEnabled,
	setEnterSendsPref,
	setQuickKeyOrder,
	setQuickKeysEnabled,
} from "../prefs";

const ENTER_CHOICES: { value: EnterSendsPref; label: string; hint: string }[] = [
	{ value: "auto", label: "Auto", hint: "send on a keyboard, newline on touch" },
	{ value: "send", label: "Always send", hint: "Enter always sends" },
	{ value: "newline", label: "Always newline", hint: "Enter always adds a line" },
];

export default function SettingsScreen() {
	const [enter, setEnter] = useState<EnterSendsPref>(enterSendsPref);
	const [keys, setKeys] = useState(quickKeysEnabled);
	const [order, setOrder] = useState(() => orderKeys(quickKeyOrder()).map((k) => k.id));
	const [version, setVersion] = useState<string | null>(null);

	useEffect(() => {
		getVersion().then(setVersion).catch(() => {});
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
			<Navbar
				left={<NavbarBackLink onClick={() => history.back()} />}
				title="Settings"
				style={{ position: "fixed", top: "var(--safe-top, 0px)", left: 0, right: 0, zIndex: 30 }}
			/>
			<div
				className="mx-auto flex min-h-dvh max-w-3xl flex-col gap-6 px-3 pt-[80px]"
				style={{ paddingBottom: "var(--safe-bottom, 0px)" }}
			>
				<section>
					<h2 className="px-1 pb-2 text-xs uppercase tracking-widest text-muted">Enter key</h2>
					<Segmented
						className="w-full"
						colors={{ strongBgMaterial: "bg-base-200", strongHighlightBgMaterial: "bg-primary" }}
					>
						{ENTER_CHOICES.map((c) => (
							<SegmentedButton
								key={c.value}
								active={enter === c.value}
								onClick={() => {
									setEnterSendsPref(c.value);
									setEnter(c.value);
								}}
								className={`text-[13px] ${enter === c.value ? "text-primary-content" : "text-base-content"}`}
							>
								{c.label}
							</SegmentedButton>
						))}
					</Segmented>
					<p className="px-1 pt-2 text-xs text-muted">
						{ENTER_CHOICES.find((c) => c.value === enter)?.hint}
					</p>
					<p className="px-1 pt-1 text-xs text-muted">
						Current effective behavior: Enter {effectiveEnterSends() ? "sends" : "adds a newline"}.
					</p>
				</section>

				<section>
					<h2 className="px-1 pb-2 text-xs uppercase tracking-widest text-muted">Quick keys</h2>
					<div className="flex items-center justify-between rounded-xl border border-muted/40 px-3 py-3">
						<span className="text-[15px]">Show the quick-keys strip in sessions</span>
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
					<div className="mt-3 overflow-hidden rounded-xl border border-muted/40">
						{order.map((id, i) => (
							<div
								key={id}
								className={`flex items-center justify-between px-3 py-1 ${i > 0 ? "border-t border-muted/20" : ""}`}
							>
								<span className="text-[15px]">
									{QUICK_KEYS.find((k) => k.id === id)?.label ?? id}
								</span>
								<span className="flex gap-1">
									<button
										type="button"
										aria-label={`Move ${id} up`}
										disabled={i === 0}
										onClick={() => move(id, -1)}
										className="min-h-[36px] min-w-[44px] rounded-lg bg-base-300 px-3 text-base-content active:scale-95 disabled:opacity-30"
									>
										↑
									</button>
									<button
										type="button"
										aria-label={`Move ${id} down`}
										disabled={i === order.length - 1}
										onClick={() => move(id, 1)}
										className="min-h-[36px] min-w-[44px] rounded-lg bg-base-300 px-3 text-base-content active:scale-95 disabled:opacity-30"
									>
										↓
									</button>
								</span>
							</div>
						))}
					</div>
					<p className="px-1 pt-2 text-xs text-muted">
						Order the keys with the arrows; sessions pick it up on the next visit.
					</p>
				</section>

				<p className="pt-2 text-center text-xs text-muted">Couch Pi {version ?? "…"}</p>
			</div>
		</div>
	);
}
