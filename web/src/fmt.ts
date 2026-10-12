export function relTime(iso: string): string {
	const t = Date.parse(iso);
	if (Number.isNaN(t)) return "";
	const s = Math.max(0, (Date.now() - t) / 1000);
	if (s < 60) return "now";
	if (s < 3600) return `${Math.floor(s / 60)}m`;
	if (s < 86400) return `${Math.floor(s / 3600)}h`;
	return `${Math.floor(s / 86400)}d`;
}

export function short(text: string, max: number): string {
	const line = text.split("\n", 1)[0].trim();
	return line.length > max ? `${line.slice(0, max - 1)}…` : line;
}
