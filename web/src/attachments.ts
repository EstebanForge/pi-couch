import { useCallback, useRef, useState } from "react";
import { uploadImage } from "./api";

export const MAX_FILES = 10;
const MAX_BYTES = 10 * 1024 * 1024;
const MAX_PARALLEL = 3;

export type Attachment = { id: number; name: string; path?: string; error?: string };
type AttachmentPatch = { path?: string; error?: string };
type AttachmentUpdate = Attachment[] | ((current: Attachment[]) => Attachment[]);

export function useAttachmentState() {
	const filesRef = useRef<Attachment[]>([]);
	const [files, setFiles] = useState<Attachment[]>([]);
	const update = useCallback((next: AttachmentUpdate) => {
		filesRef.current = typeof next === "function" ? next(filesRef.current) : next;
		setFiles(filesRef.current);
	}, []);
	return { files, filesRef, setFiles: update };
}

export function prepareAttachments(
	list: FileList | null,
	currentCount: number,
	nextID: () => number,
): { picked: File[]; batch: Attachment[]; dropped: number } {
	const files = list ? [...list] : [];
	const picked = files.slice(0, Math.max(0, MAX_FILES - currentCount));
	return {
		picked,
		batch: picked.map((file) => ({ id: nextID(), name: file.name })),
		dropped: files.length - picked.length,
	};
}

let activeUploads = 0;
const uploadWaiters: Array<() => void> = [];

async function acquireUploadSlot(): Promise<void> {
	if (activeUploads >= MAX_PARALLEL || uploadWaiters.length > 0) {
		await new Promise<void>((resolve) => uploadWaiters.push(resolve));
		return;
	}
	activeUploads++;
}

function releaseUploadSlot() {
	const next = uploadWaiters.shift();
	if (next) {
		// The waiter inherits this slot; decrementing first would oversubscribe it.
		next();
		return;
	}
	activeUploads--;
}

export async function uploadAttachments(
	files: readonly File[],
	batch: readonly Attachment[],
	update: (id: number, patch: AttachmentPatch) => void,
): Promise<void> {
	await Promise.all(
		files.map(async (file, index) => {
			const attachment = batch[index];
			if (file.size > MAX_BYTES) {
				update(attachment.id, { error: "over 10 MB" });
				return;
			}
			await acquireUploadSlot();
			try {
				update(attachment.id, { path: await uploadImage(file) });
			} catch {
				update(attachment.id, { error: "upload failed" });
			} finally {
				releaseUploadSlot();
			}
		}),
	);
}
