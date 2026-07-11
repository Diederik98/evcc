<template>
	<div class="peak-shave-settings">
		<div
			v-if="!limitControllerAvailable"
			class="alert alert-warning mb-4"
			role="alert"
		>
			{{ $t("peakShave.noLimitController") }}
		</div>

		<!-- State indicator -->
		<div class="d-flex align-items-center gap-2 mb-4">
			<div class="state-dot" :class="`state-dot--${peakShaveState}`"></div>
			<span class="fw-bold">{{ $t("peakShave.state." + peakShaveState) }}</span>
		</div>

		<!-- Load shed delay -->
		<div class="mb-4">
			<label for="peakShaveLoadShedDelay" class="form-label fw-bold">
				{{ $t("peakShave.loadShedDelay") }}
			</label>
			<p class="text-muted small mb-2">{{ $t("peakShave.loadShedDelayHelp") }}</p>
			<div class="input-group">
				<input
					id="peakShaveLoadShedDelay"
					v-model.number="localLoadShedDelay"
					type="number"
					min="0"
					step="5"
					class="form-control"
					@change="saveLoadShedDelay"
				/>
				<span class="input-group-text">s</span>
			</div>
		</div>

		<!-- Maintain SoC Charge Power -->
		<div class="mb-4">
			<label for="peakShaveMaintainPower" class="form-label fw-bold">
				{{ $t("peakShave.maintainSocChargePower") }}
			</label>
			<p class="text-muted small mb-2">{{ $t("peakShave.maintainSocChargePowerHelp") }}</p>
			<div class="input-group">
				<input
					id="peakShaveMaintainPower"
					v-model.number="localMaintainPower"
					type="number"
					min="0"
					step="100"
					class="form-control"
					@change="saveMaintainPower"
				/>
				<span class="input-group-text">W</span>
			</div>
		</div>

		<!-- Reserve SoC bar -->
		<div class="mb-2">
			<label class="form-label fw-bold">{{ $t("peakShave.reserveSoc") }}</label>
			<p class="text-muted small mb-2">{{ $t("peakShave.reserveSocHelp") }}</p>
		</div>
		<div class="soc-bar-container mb-2">
			<div ref="socBar" class="soc-bar soc-bar--interactive">
				<div class="soc-bar__min" :style="{ width: peakShaveMinSoc + '%' }"></div>
				<div
					class="soc-bar__reserve"
					:style="{ left: peakShaveMinSoc + '%', width: reserveBandWidth + '%' }"
				></div>
				<span
					class="soc-bar__handle soc-bar__handle--min"
					:style="{ left: peakShaveMinSoc + '%' }"
					:title="$t('peakShave.minSocTooltip', { soc: peakShaveMinSoc })"
					:aria-label="$t('peakShave.minSocTooltip', { soc: peakShaveMinSoc })"
					role="img"
				></span>
				<button
					type="button"
					class="soc-bar__handle soc-bar__handle--reserve"
					:class="{ 'soc-bar__handle--dragging': dragging }"
					:style="{ left: dragReserveSoc + '%' }"
					:title="$t('peakShave.reserveSocTooltip', { soc: dragReserveSoc })"
					:aria-label="$t('peakShave.reserveSocTooltip', { soc: dragReserveSoc })"
					:aria-valuenow="dragReserveSoc"
					:aria-valuemin="peakShaveMinSoc"
					aria-valuemax="100"
					role="slider"
					@mousedown.prevent="startDrag"
					@touchstart.prevent="startDrag"
					@keydown="onReserveKeydown"
				></button>
			</div>
			<div class="d-flex justify-content-between small text-muted mt-2">
				<span>0%</span>
				<span>{{ $t("peakShave.minSocFromBattery", { soc: peakShaveMinSoc }) }}</span>
				<span>{{ $t("peakShave.reserveSocLabel") }}: {{ dragReserveSoc }}%</span>
				<span>100%</span>
			</div>
		</div>
	</div>
</template>

<script lang="ts">
import { defineComponent, type PropType } from "vue";
import api from "@/api";
import type { PeakShaveState } from "@/types/evcc";

export default defineComponent({
	name: "BatteryPeakShaveSettings",
	props: {
		peakShaveReserveSoc: { type: Number, default: 40 },
		peakShaveMinSoc: { type: Number, default: 20 },
		peakShaveMaintainSocChargePower: { type: Number, default: 1000 },
		peakShaveLoadShedDelay: { type: Number, default: 30 },
		peakShaveState: { type: String as PropType<PeakShaveState>, default: "idle" },
		limitControllerAvailable: { type: Boolean, default: true },
	},
	data() {
		return {
			localReserveSoc: this.peakShaveReserveSoc,
			localMaintainPower: this.peakShaveMaintainSocChargePower,
			localLoadShedDelay: this.peakShaveLoadShedDelay,
			dragging: false,
		};
	},
	computed: {
		dragReserveSoc(): number {
			return this.localReserveSoc;
		},
		reserveBandWidth(): number {
			return Math.max(0, this.localReserveSoc - this.peakShaveMinSoc);
		},
	},
	watch: {
		peakShaveReserveSoc(v: number) {
			if (!this.dragging) {
				this.localReserveSoc = v;
			}
		},
		peakShaveMaintainSocChargePower(v: number) {
			this.localMaintainPower = v;
		},
		peakShaveLoadShedDelay(v: number) {
			this.localLoadShedDelay = v;
		},
	},
	beforeUnmount() {
		this.stopDrag();
	},
	methods: {
		snapSoc(value: number): number {
			const min = this.peakShaveMinSoc;
			const snapped = Math.round(value / 5) * 5;
			return Math.min(100, Math.max(min, snapped));
		},
		socFromPointer(clientX: number): number {
			const bar = this.$refs.socBar as HTMLElement | undefined;
			if (!bar) {
				return this.localReserveSoc;
			}
			const rect = bar.getBoundingClientRect();
			const ratio = (clientX - rect.left) / rect.width;
			return this.snapSoc(ratio * 100);
		},
		startDrag(event: MouseEvent | TouchEvent) {
			this.dragging = true;
			const move = (e: MouseEvent | TouchEvent) => {
				const clientX = "touches" in e ? e.touches[0]?.clientX : e.clientX;
				if (clientX === undefined) {
					return;
				}
				this.localReserveSoc = this.socFromPointer(clientX);
			};
			const stop = async () => {
				document.removeEventListener("mousemove", move);
				document.removeEventListener("mouseup", stop);
				document.removeEventListener("touchmove", move);
				document.removeEventListener("touchend", stop);
				this.dragging = false;
				await this.saveReserveSoc();
			};
			document.addEventListener("mousemove", move);
			document.addEventListener("mouseup", stop);
			document.addEventListener("touchmove", move, { passive: false });
			document.addEventListener("touchend", stop);
			move(event);
		},
		stopDrag() {
			this.dragging = false;
		},
		onReserveKeydown(event: KeyboardEvent) {
			let next = this.localReserveSoc;
			if (event.key === "ArrowRight" || event.key === "ArrowUp") {
				next += 5;
			} else if (event.key === "ArrowLeft" || event.key === "ArrowDown") {
				next -= 5;
			} else {
				return;
			}
			event.preventDefault();
			this.localReserveSoc = this.snapSoc(next);
			this.saveReserveSoc();
		},
		validateReserveSoc(): boolean {
			if (this.localReserveSoc < this.peakShaveMinSoc) {
				window.app.raise(this.$t("peakShave.socInvalid"));
				return false;
			}
			return true;
		},
		async saveReserveSoc() {
			if (!this.validateReserveSoc()) {
				return;
			}
			try {
				await api.post(`peakshavereservesoc/${encodeURIComponent(this.localReserveSoc)}`);
			} catch (err) {
				console.error(err);
			}
		},
		async saveMaintainPower() {
			try {
				await api.post(
					`peakshavemaintainsocchargepower/${encodeURIComponent(this.localMaintainPower)}`
				);
			} catch (err) {
				console.error(err);
			}
		},
		async saveLoadShedDelay() {
			try {
				await api.post(
					`peakshaveloadsheddelay/${encodeURIComponent(this.localLoadShedDelay)}`
				);
			} catch (err) {
				console.error(err);
			}
		},
	},
});
</script>

<style scoped>
.state-dot {
	width: 12px;
	height: 12px;
	border-radius: 50%;
	flex-shrink: 0;
	background: var(--evcc-gray);
}
.state-dot--shaving {
	background: var(--evcc-orange, #f97316);
}
.state-dot--critical {
	background: var(--evcc-orange, #f97316);
}
.state-dot--shedding {
	background: var(--evcc-red, #ef4444);
	animation: pulse 1s infinite;
}
.state-dot--lockout {
	background: var(--evcc-red, #ef4444);
	animation: pulse 1s infinite;
}
.state-dot--recovery {
	background: var(--evcc-green, #22c55e);
}
.state-dot--idle {
	background: var(--evcc-gray, #9ca3af);
}
@keyframes pulse {
	0%,
	100% {
		opacity: 1;
	}
	50% {
		opacity: 0.4;
	}
}

.soc-bar-container {
	background: var(--evcc-background, #f3f4f6);
	border-radius: 8px;
	padding: 12px 12px 8px;
}
.soc-bar {
	position: relative;
	height: 16px;
	background: var(--evcc-box, #e5e7eb);
	border-radius: 8px;
	overflow: visible;
}
.soc-bar--interactive {
	margin-top: 10px;
	margin-bottom: 10px;
}
.soc-bar__min {
	position: absolute;
	left: 0;
	top: 0;
	height: 100%;
	background: var(--evcc-red, #ef4444);
	opacity: 0.5;
	border-radius: 8px 0 0 8px;
	pointer-events: none;
}
.soc-bar__reserve {
	position: absolute;
	top: 0;
	height: 100%;
	background: var(--evcc-orange, #f97316);
	opacity: 0.5;
	pointer-events: none;
}
.soc-bar__handle {
	position: absolute;
	top: 50%;
	width: 18px;
	height: 18px;
	margin: 0;
	padding: 0;
	border: 2px solid var(--evcc-background, #fff);
	border-radius: 50%;
	transform: translate(-50%, -50%);
	box-shadow: 0 1px 3px rgb(0 0 0 / 20%);
	cursor: grab;
	z-index: 2;
}
.soc-bar__handle--min {
	background: var(--evcc-red, #ef4444);
	cursor: default;
	opacity: 0.9;
}
.soc-bar__handle--reserve {
	background: var(--evcc-orange, #f97316);
}
.soc-bar__handle--reserve:focus-visible {
	outline: 2px solid var(--evcc-green, #22c55e);
	outline-offset: 2px;
}
.soc-bar__handle--dragging,
.soc-bar__handle--reserve:active {
	cursor: grabbing;
}
</style>
