import type { ReactNode } from "react";
import { FaChevronLeft } from "./fa";

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
			className="header-glass fixed inset-x-0 top-0 z-30 border-b border-base-content/8 shadow-surface-sm"
			style={{ paddingTop: "var(--safe-top, 0px)" }}
		>
			<div className="mx-auto flex max-w-3xl items-center gap-2 px-3 py-2.5">
				{props.onBack && (
					<button
						type="button"
						aria-label="Back"
						onClick={props.onBack}
						className="press-scale grid h-11 w-11 shrink-0 place-items-center rounded-xl text-muted active:text-base-content"
					>
						<FaChevronLeft size={20} />
					</button>
				)}
				<div className="min-w-0 flex-1">
					<p className="truncate text-[16px] font-semibold tracking-tight leading-tight text-base-content">
						{props.title}
					</p>
					{props.subtitle && (
						<p className="mt-0.5 truncate text-[11.5px] leading-tight text-muted">
							{props.subtitle}
						</p>
					)}
				</div>
				{props.right}
			</div>
		</header>
	);
}
