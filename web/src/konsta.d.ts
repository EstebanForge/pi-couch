declare module "konsta/shared/utils" {
	export class TouchRipple {
		constructor(el: HTMLElement, x: number, y: number);
		remove(): void;
		destroy(): void;
	}
}
