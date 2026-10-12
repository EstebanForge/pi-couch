type Props = { sessionKey: string };

// Stage 1 stub: proves push, Back and the slide. The chat lands in stage 2.
export default function Session({ sessionKey }: Props) {
	return (
		<div className="mx-auto flex min-h-dvh max-w-3xl flex-col">
			<header className="sticky top-0 z-10 flex items-center gap-2 bg-base-300/95 px-2 py-3 backdrop-blur">
				<button
					type="button"
					aria-label="Back"
					onClick={() => history.back()}
					className="grid h-11 w-11 place-items-center rounded-xl text-muted"
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
				<h1 className="truncate text-[15px] font-medium">{sessionKey}</h1>
			</header>
			<main className="flex-1 px-4">
				<p className="mt-6 text-sm text-muted">Stub screen. The chat lands in stage 2.</p>
			</main>
		</div>
	);
}
