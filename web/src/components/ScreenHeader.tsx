import type { ReactNode } from "react";

// Konsta's Navbar pins below the safe-area inset, leaving a transparent
// strip above it where scrolled content ghosts through, and renders
// oversized in the dracula theme. Same fixed header as HomeList: top-0,
// inset padding inside, opaque bar.
export function ScreenHeader(props: {
	title: string;
	subtitle?: string;
	onBack?: () => void;
	right?: ReactNode;
}) {
	return (
		<header
			className="fixed inset-x-0 top-0 z-30 bg-base-300/95 backdrop-blur"
			style={{ paddingTop: "var(--safe-top, 0px)" }}
		>
			<div className="mx-auto flex max-w-3xl items-center gap-2 px-3 py-2">
				{props.onBack && (
					<button
						type="button"
						aria-label="Back"
						onClick={props.onBack}
						className="grid h-11 w-11 shrink-0 place-items-center rounded-xl text-muted active:scale-95"
					>
						<svg width="20" height="20" viewBox="0 0 20 20" aria-hidden="true">
							<path
								d="M12.5 4 6.5 10l6 6"
								stroke="currentColor"
								strokeWidth="2"
								strokeLinecap="round"
								strokeLinejoin="round"
								fill="none"
							/>
						</svg>
					</button>
				)}
				<div className="min-w-0 flex-1">
					<p className="truncate text-[15px] font-medium leading-tight">{props.title}</p>
					{props.subtitle && <p className="truncate text-[11px] leading-tight text-muted">{props.subtitle}</p>}
				</div>
				{props.right}
			</div>
		</header>
	);
}
