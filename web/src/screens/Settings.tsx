// Settings.tsx — /settings: per-device UI prefs. Reached from the home
// side menu. Values live in localStorage (the settings screen writes,
// the composer reads at mount), so they follow the browser/device, not
// the session. Konsta supplies the controls (Segmented, Toggle); List
// stays rejected like everywhere else (material look fights dracula).
import { Navbar, NavbarBackLink, Segmented, SegmentedButton, Toggle } from "konsta/react";
import { useState } from "react";
import {
	type EnterSendsPref,
	effectiveEnterSends,
	enterSendsPref,
	quickKeysEnabled,
	setEnterSendsPref,
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
				</section>
			</div>
		</div>
	);
}
