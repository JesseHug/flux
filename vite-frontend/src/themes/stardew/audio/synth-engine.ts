import type { SceneKey } from "../assets";

interface SynthPattern {
  notes: readonly number[];
  bass: readonly number[];
  tempoMs: number;
  wave: OscillatorType;
}

const PATTERNS: Record<SceneKey, SynthPattern> = {
  "spring-day": {
    notes: [523.25, 659.25, 783.99, 659.25, 880.0, 783.99, 659.25, 587.33],
    bass: [261.63, 196.0, 220.0, 196.0],
    tempoMs: 430,
    wave: "triangle",
  },
  "spring-night": {
    notes: [440.0, 523.25, 659.25, 783.99, 659.25, 587.33, 523.25, 440.0],
    bass: [220.0, 174.61, 196.0, 164.81],
    tempoMs: 620,
    wave: "sine",
  },
  "summer-day": {
    notes: [587.33, 739.99, 880.0, 987.77, 880.0, 739.99, 659.25, 587.33],
    bass: [293.66, 220.0, 246.94, 220.0],
    tempoMs: 380,
    wave: "triangle",
  },
  "summer-night": {
    notes: [392.0, 493.88, 587.33, 739.99, 587.33, 493.88, 440.0, 392.0],
    bass: [196.0, 164.81, 146.83, 196.0],
    tempoMs: 680,
    wave: "sine",
  },
  "fall-day": {
    notes: [440.0, 523.25, 587.33, 659.25, 587.33, 523.25, 493.88, 392.0],
    bass: [220.0, 174.61, 196.0, 146.83],
    tempoMs: 480,
    wave: "triangle",
  },
  "fall-night": {
    notes: [349.23, 440.0, 523.25, 493.88, 440.0, 392.0, 349.23, 329.63],
    bass: [174.61, 146.83, 130.81, 164.81],
    tempoMs: 640,
    wave: "sine",
  },
  "winter-day": {
    notes: [523.25, 587.33, 659.25, 783.99, 659.25, 587.33, 493.88, 440.0],
    bass: [261.63, 220.0, 196.0, 174.61],
    tempoMs: 540,
    wave: "sine",
  },
  "winter-night": {
    notes: [392.0, 440.0, 523.25, 659.25, 523.25, 440.0, 392.0, 329.63],
    bass: [196.0, 146.83, 130.81, 164.81],
    tempoMs: 720,
    wave: "sine",
  },
};

/** Built-in 16-bit Web Audio pastoral synthesizer used when .ogg tracks are absent or selected. */
export class PastoralSynthEngine {
  private ctx: AudioContext | null = null;
  private master: GainNode | null = null;
  private timer: number | null = null;
  private fadeTimer: number | null = null;
  private step = 0;
  private sceneKey: SceneKey = "spring-day";
  private playing = false;
  private volume = 0.45;

  public setVolume(value: number): void {
    this.volume = Math.max(0, Math.min(1, value));
    if (this.ctx && this.master) {
      this.master.gain.setTargetAtTime(this.volume, this.ctx.currentTime, 0.04);
    }
  }

  public start(
    scene: SceneKey,
    volume: number,
    sharedCtx?: AudioContext,
  ): void {
    this.volume = Math.max(0, Math.min(1, volume));
    this.sceneKey = scene;
    this.ensureContext(sharedCtx);
    if (!this.ctx || !this.master) return;

    this.playing = true;
    this.master.gain.setTargetAtTime(this.volume, this.ctx.currentTime, 0.25);
    this.restartLoop();
  }

  public switchScene(scene: SceneKey): void {
    if (this.sceneKey === scene) return;
    this.sceneKey = scene;
    if (!this.playing || !this.ctx || !this.master) return;

    this.master.gain.setTargetAtTime(0.02, this.ctx.currentTime, 0.3);
    if (this.fadeTimer !== null) window.clearTimeout(this.fadeTimer);
    this.fadeTimer = window.setTimeout(() => {
      if (!this.playing || !this.ctx || !this.master) return;
      this.restartLoop();
      this.master.gain.setTargetAtTime(this.volume, this.ctx.currentTime, 0.45);
    }, 450);
  }

  public stop(): void {
    this.playing = false;
    if (this.fadeTimer !== null) {
      window.clearTimeout(this.fadeTimer);
      this.fadeTimer = null;
    }
    if (this.timer !== null) {
      window.clearInterval(this.timer);
      this.timer = null;
    }
    if (this.ctx && this.master) {
      this.master.gain.setTargetAtTime(0.0001, this.ctx.currentTime, 0.1);
    }
  }

  private ensureContext(sharedCtx?: AudioContext): void {
    if (sharedCtx) {
      this.ctx = sharedCtx;
    } else if (!this.ctx && typeof window !== "undefined") {
      const AC =
        window.AudioContext ||
        (window as unknown as { webkitAudioContext: typeof AudioContext })
          .webkitAudioContext;

      this.ctx = new AC();
    }
    if (this.ctx && !this.master) {
      this.master = this.ctx.createGain();
      this.master.gain.value = this.volume;
      this.master.connect(this.ctx.destination);
    }
    if (this.ctx?.state === "suspended") {
      void this.ctx.resume().catch(() => {});
    }
  }

  private restartLoop(): void {
    if (this.timer !== null) window.clearInterval(this.timer);
    const pattern = PATTERNS[this.sceneKey];

    this.playTick();
    this.timer = window.setInterval(() => this.playTick(), pattern.tempoMs);
  }

  private playTick(): void {
    if (!this.playing || !this.ctx || !this.master) return;
    if (this.ctx.state !== "running") return;

    const pattern = PATTERNS[this.sceneKey];
    const now = this.ctx.currentTime;
    const freq = pattern.notes[this.step % pattern.notes.length];
    const durSec = pattern.tempoMs / 1000;

    const osc = this.ctx.createOscillator();
    const gain = this.ctx.createGain();
    const filter = this.ctx.createBiquadFilter();

    osc.type = pattern.wave;
    osc.frequency.setValueAtTime(freq, now);
    filter.type = "lowpass";
    filter.frequency.setValueAtTime(1750, now);

    gain.gain.setValueAtTime(0.001, now);
    gain.gain.linearRampToValueAtTime(0.18, now + 0.03);
    gain.gain.exponentialRampToValueAtTime(0.001, now + durSec * 1.4);

    osc.connect(filter);
    filter.connect(gain);
    gain.connect(this.master);
    osc.start(now);
    osc.stop(now + durSec * 1.45);

    if (this.step % 2 === 0) {
      const bFreq =
        pattern.bass[Math.floor(this.step / 2) % pattern.bass.length];
      const bOsc = this.ctx.createOscillator();
      const bGain = this.ctx.createGain();

      bOsc.type = "triangle";
      bOsc.frequency.setValueAtTime(bFreq, now);
      bGain.gain.setValueAtTime(0.001, now);
      bGain.gain.linearRampToValueAtTime(0.12, now + 0.08);
      bGain.gain.exponentialRampToValueAtTime(0.001, now + durSec * 2.1);
      bOsc.connect(bGain);
      bGain.connect(this.master);
      bOsc.start(now);
      bOsc.stop(now + durSec * 2.2);
    }

    this.step++;
  }
}
