// Dumps pi's merged model catalog with each model's supported thinking
// levels. Must run under pi's own node against the CURRENT release
// root (argv[1]) so the level rules — the reasoning flag, null entries
// in thinkingLevelMap, explicit-only xhigh/max — stay pi's code, not a
// copy that drifts. Prints [{id, reasoning, levels}] as one JSON array.
process.env.PI_OFFLINE = "1";
const root = process.argv[2];
if (!root) {
	console.error("usage: models_dump.mjs <release-root>");
	process.exit(1);
}
const rtMod = await import(
	new URL(`file://${root}/node_modules/@earendil-works/pi-coding-agent/dist/core/model-runtime.js`).href
);
const aiMod = await import(
	new URL(`file://${root}/node_modules/@earendil-works/pi-ai/dist/models.js`).href
);
const rt = await rtMod.ModelRuntime.create({ refreshOnCreate: false });
const models = await rt.getAvailable(undefined, {});
const rows = [];
for (const m of models) {
	rows.push({
		id: `${m.provider}/${m.id}`,
		reasoning: !!m.reasoning,
		levels: aiMod.getSupportedThinkingLevels(m),
	});
}
console.log(JSON.stringify(rows));
