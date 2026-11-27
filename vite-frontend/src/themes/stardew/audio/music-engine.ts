import { musicUrl, type AudioEnginePref, type SceneKey } from "../assets";

import { PastoralSynthEngine } from "./synth-engine";

export type MusicStatus = "off" | "loading" | "playing" | "blocked";
export type ActiveEngine = "ogg" | "synth";

export interface MusicSnapshot {
  enabled: boolean;
  status: MusicStatus;
  activeEngine: ActiveEngine;
  scene: SceneKey;
  volume: number;
}

interface Voice {
  id: SceneKey;
  source: AudioBufferSourceNode;
  gain: GainNode;
  offset: number;
  started: number;
  envelope: { from: number; to: number; time: number; duration: number };
}

/**
 * Dual-engine Stardew BGM player:
 * 1. Prefers the 8 seasonal .ogg tracks with 3-second WebAudio crossfades.
 * 2. Falls back cleanly to the built-in 16-bit pastoral synthesizer if .ogg files
 *    have not been fetched locally or fail to decode.
 */
export class StardewMusicEngine {
  private context: AudioContext | null = null;
  private master: GainNode | null = null;
  private synth = new PastoralSynthEngine();
  private cache = new Map<string, Promise<AudioBuffer>>();
  private voices = new Set<Voice>();
  private activeVoice: Voice | null = null;
  private resumePoint: { id: SceneKey; offset: number } | null = null;

  private enabled = false;
  private desired: SceneKey = "spring-day";
  private pref: AudioEnginePref = "auto";
  private customBase = "";
  private volume = 0.45;
  private fadeSeconds = 3;
  private requestSeq = 0;
  private loadingSeq: number | null = null;
  private status: MusicStatus = "off";
  private activeEngine: ActiveEngine = "ogg";
  private onState: (snap: MusicSnapshot) => void;

  constructor(options: {
    volume?: number;
    onState: (snap: MusicSnapshot) => void;
  }) {
    this.volume = options.volume ?? 0.45;
    this.onState = options.onState;
  }

  public configure(pref: AudioEnginePref, customBase: string): void {
    const changed = this.pref !== pref || this.customBase !== customBase;

    this.pref = pref;
    if (this.customBase !== customBase) {
      this.customBase = customBase;
      this.cache.clear();
    }
    if (changed && this.enabled) {
      this.requestSeq++;
      void this.playCurrent();
    }
  }

  public setScene(scene: SceneKey): void {
    if (scene === this.desired) return;
    this.desired = scene;
    this.requestSeq++;
    if (this.enabled && this.context?.state === "running") {
      void this.playCurrent();
    } else {
      this.emit(this.enabled ? "blocked" : "off");
    }
  }

  public enable(): void {
    this.enabled = true;
    this.requestSeq++;
    try {
      this.ensureContext();
      if (!this.context) {
        this.emit("blocked");

        return;
      }
      if (this.context.state !== "running") {
        this.emit("blocked");
        void Promise.resolve(this.context.resume()).catch(() => {
          if (this.enabled) this.emit("blocked");
        });
      } else {
        void this.playCurrent();
      }
    } catch {
      this.startSynthFallback();
    }
  }

  public disable(): void {
    this.enabled = false;
    this.requestSeq++;
    if (this.activeVoice) {
      this.resumePoint = {
        id: this.activeVoice.id,
        offset: this.voiceOffset(this.activeVoice),
      };
      this.activeVoice = null;
    }
    for (const v of this.voices) {
      this.retireVoice(v, 0.18);
    }
    this.synth.stop();
    this.emit("off");
  }

  public setVolume(value: number): void {
    this.volume = Math.max(0, Math.min(1, value));
    if (this.context && this.master) {
      this.master.gain.setTargetAtTime(
        this.volume,
        this.context.currentTime,
        0.04,
      );
    }
    this.synth.setVolume(this.volume);
    this.emit(this.status);
  }

  public dispose(): void {
    this.disable();
    this.cache.clear();
    if (this.context && this.context.state !== "closed") {
      void this.context.close().catch(() => {});
    }
    this.context = null;
    this.master = null;
  }

  private emit(status: MusicStatus): void {
    this.status = status;
    this.onState({
      enabled: this.enabled,
      status,
      activeEngine: this.activeEngine,
      scene: this.desired,
      volume: this.volume,
    });
  }

  private ensureContext(): void {
    if (this.context || typeof window === "undefined") return;
    const AC =
      window.AudioContext ||
      (window as unknown as { webkitAudioContext: typeof AudioContext })
        .webkitAudioContext;

    this.context = new AC();
    this.master = this.context.createGain();
    this.master.gain.value = this.volume;
    this.master.connect(this.context.destination);
    this.context.onstatechange = () => {
      if (!this.enabled) return;
      if (this.context?.state === "running") {
        void this.playCurrent();
      } else {
        this.emit("blocked");
      }
    };
  }

  private voiceOffset(v: Voice): number {
    if (!this.context || !v.source.buffer) return 0;

    return (
      (v.offset + this.context.currentTime - v.started) %
      v.source.buffer.duration
    );
  }

  private rampVoice(v: Voice, to: number, duration: number): void {
    if (!this.context) return;
    const now = this.context.currentTime;
    const prior = v.envelope;
    const mix = Math.max(
      0,
      Math.min(1, (now - prior.time) / (prior.duration || 1)),
    );
    const from = prior.from + (prior.to - prior.from) * mix;

    v.gain.gain.cancelScheduledValues(now);
    v.gain.gain.setValueAtTime(from, now);
    v.gain.gain.linearRampToValueAtTime(to, now + duration);
    v.envelope = { from, to, time: now, duration };
  }

  private retireVoice(v: Voice, duration: number): void {
    if (!this.context) return;
    this.rampVoice(v, 0, duration);
    try {
      v.source.stop(this.context.currentTime + duration + 0.03);
    } catch {
      // already stopped
    }
  }

  private async loadBuffer(scene: SceneKey): Promise<AudioBuffer> {
    if (!this.context) throw new Error("No AudioContext");
    const url = musicUrl(scene, this.customBase);
    const cacheKey = `${scene}|${url}`;
    const hit = this.cache.get(cacheKey);

    if (hit) {
      this.cache.delete(cacheKey);
      this.cache.set(cacheKey, hit);

      return hit;
    }

    const ctx = this.context;
    const promise = fetch(url)
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);

        return res.arrayBuffer();
      })
      .then((buf) => ctx.decodeAudioData(buf));

    this.cache.set(cacheKey, promise);
    while (this.cache.size > 3) {
      const oldest = this.cache.keys().next().value;

      if (oldest) this.cache.delete(oldest);
      else break;
    }

    try {
      return await promise;
    } catch (err) {
      if (this.cache.get(cacheKey) === promise) this.cache.delete(cacheKey);
      throw err;
    }
  }

  private startSynthFallback(): void {
    this.activeEngine = "synth";
    for (const v of this.voices) {
      this.retireVoice(v, 0.2);
    }
    this.activeVoice = null;
    this.synth.start(this.desired, this.volume, this.context ?? undefined);
    this.emit("playing");
  }

  private async playCurrent(): Promise<void> {
    const scene = this.desired;
    const req = this.requestSeq;

    if (!this.enabled || !this.context || this.context.state !== "running") {
      return;
    }

    if (this.pref === "synth") {
      if (this.activeEngine === "synth") {
        this.synth.switchScene(scene);
        this.emit("playing");
      } else {
        this.startSynthFallback();
      }

      return;
    }

    if (this.activeVoice?.id === scene && this.activeEngine === "ogg") {
      this.emit("playing");

      return;
    }
    if (this.loadingSeq === req) return;
    this.loadingSeq = req;
    this.emit("loading");

    try {
      const buffer = await this.loadBuffer(scene);

      if (
        req !== this.requestSeq ||
        !this.enabled ||
        !this.context ||
        !this.master ||
        this.context.state !== "running"
      ) {
        return;
      }

      this.synth.stop();
      this.activeEngine = "ogg";

      const source = this.context.createBufferSource();
      const gain = this.context.createGain();
      const now = this.context.currentTime;
      const offset =
        this.resumePoint?.id === scene
          ? this.resumePoint.offset % buffer.duration
          : 0;
      const voice: Voice = {
        id: scene,
        source,
        gain,
        offset,
        started: now,
        envelope: { from: 0, to: 0, time: now, duration: 0 },
      };

      source.buffer = buffer;
      source.loop = true;
      gain.gain.setValueAtTime(0, now);
      source.connect(gain);
      gain.connect(this.master);

      this.voices.add(voice);
      source.onended = () => {
        this.voices.delete(voice);
        if (this.activeVoice === voice) this.activeVoice = null;
      };

      const fade = this.activeVoice ? this.fadeSeconds : 0.45;

      for (const prev of this.voices) {
        if (prev !== voice) this.retireVoice(prev, fade);
      }
      source.start(now, offset);
      this.rampVoice(voice, 1, fade);
      this.activeVoice = voice;
      this.resumePoint = null;
      this.emit("playing");
    } catch {
      if (req !== this.requestSeq || !this.enabled) return;
      this.startSynthFallback();
    } finally {
      if (this.loadingSeq === req) this.loadingSeq = null;
    }
  }
}
