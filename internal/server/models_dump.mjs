// Dumps pi's merged model catalog with each model's supported thinking
// levels. Must run under pi's own node against the CURRENT release
// root (argv[1]) so the level rules — the reasoning flag, null entries
// in thinkingLevelMap, explicit-only xhigh/max — stay pi's code, not a
// copy that drifts. Prints [{id, levels}] as one JSON array.
import { pathToFileURL } from "node:url";

process.env.PI_OFFLINE = "1";
const root = process.argv[2];
if (!root) {
	console.error("usage: models_dump.mjs <release-root>");
	process.exit(1);
}
const mod = (rel) => pathToFileURL(`${root}/node_modules/${rel}`).href;
const rtMod = await import(
	mod("@earendil-works/pi-coding-agent/dist/core/model-runtime.js")
);
// pi-coding-agent may nest its own pi-ai; prefer that so the level
// rules match the copy the agent actually runs.
const aiPath = "@earendil-works/pi-ai/dist/models.js";
let aiMod;
try {
	aiMod = await import(
		mod(`@earendil-works/pi-coding-agent/node_modules/${aiPath}`)
	);
} catch {
	aiMod = await import(mod(aiPath));
}
const rt = await rtMod.ModelRuntime.create({ refreshOnCreate: false });
const models = await rt.getAvailable(undefined, {});
const rows = [];
for (const m of models) {
	rows.push({
		id: `${m.provider}/${m.id}`,
		levels: aiMod.getSupportedThinkingLevels(m),
	});
}
console.log(JSON.stringify(rows));
process.exit(0);
