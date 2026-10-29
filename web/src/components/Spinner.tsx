import { useId } from "react";
import { type SpinnerStyle, spinnerStylePref } from "../prefs";

// The one waiting indicator for the whole app: svg-spinners "blocks
// shuffle" (MIT, Utkarsh Verma — github.com/n3r4zzurr0/svg-spinners).
// SMIL ids must be unique per instance, so each mount derives its own.
// prefers-reduced-motion renders the static blocks with no animation.

const REDUCED =
	typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

export function Spinner({
	size = 14,
	className = "",
	variant,
}: {
	size?: number;
	className?: string;
	variant?: SpinnerStyle;
}) {
	const uid = useId();
	if ((variant ?? spinnerStylePref()) === "squares") {
		return <SpinnerSquares size={size} className={className} />;
	}
	const id = (n: string) => `${uid}${n}`;
	const begin = (n: string) => `${id(n)}.end`;
	const rect = (x: number, y: number, anims: React.ReactNode) => (
		<rect width="10" height="10" x={x} y={y} fill="currentColor" rx="1">
			{anims}
		</rect>
	);
	return (
		<svg
			width={size}
			height={size}
			viewBox="0 0 24 24"
			className={`inline-block shrink-0 align-[-2px] ${className}`}
			role="img"
			aria-label="waiting"
		>
			{rect(
				1,
				1,
				!REDUCED && (
					<>
						<animate
							id={id("a1")}
							fill="freeze"
							attributeName="x"
							begin={`0;${begin("a8")}`}
							dur="0.2s"
							values="1;13"
						/>
						<animate
							id={id("a2")}
							fill="freeze"
							attributeName="y"
							begin={begin("a5")}
							dur="0.2s"
							values="1;13"
						/>
						<animate
							id={id("a3")}
							fill="freeze"
							attributeName="x"
							begin={begin("a6")}
							dur="0.2s"
							values="13;1"
						/>
						<animate
							id={id("a4")}
							fill="freeze"
							attributeName="y"
							begin={begin("a7")}
							dur="0.2s"
							values="13;1"
						/>
					</>
				),
			)}
			{rect(
				1,
				13,
				!REDUCED && (
					<>
						<animate
							id={id("a5")}
							fill="freeze"
							attributeName="y"
							begin={begin("a1")}
							dur="0.2s"
							values="13;1"
						/>
						<animate
							id={id("a6")}
							fill="freeze"
							attributeName="x"
							begin={begin("a2")}
							dur="0.2s"
							values="1;13"
						/>
						<animate
							id={id("a7")}
							fill="freeze"
							attributeName="y"
							begin={begin("a3")}
							dur="0.2s"
							values="1;13"
						/>
						<animate
							id={id("a8")}
							fill="freeze"
							attributeName="x"
							begin={begin("a4")}
							dur="0.2s"
							values="13;1"
						/>
					</>
				),
			)}
		</svg>
	);
}

// Alternate style, pickable in Settings: the pure-CSS two-square
// rotator. Squares draw as inset currentColor rings so status colors
// (yellow waiting, theme primary) carry over; the span's height and
// the --sq-* vars scale every part off the same size prop as the svg.
function SpinnerSquares({ size, className }: { size: number; className: string }) {
	return (
		<span
			role="img"
			aria-label="waiting"
			className={`spinner-squares inline-block shrink-0 align-[-2px] ${className}`}
			style={{
				height: Math.max(4, Math.round(size * 0.4)),
				gap: size * 0.22,
				["--sq-ring" as string]: `${(size * 0.12).toFixed(2)}px`,
				["--sq-slide" as string]: `${(size * 0.11).toFixed(2)}px`,
			}}
		/>
	);
}
