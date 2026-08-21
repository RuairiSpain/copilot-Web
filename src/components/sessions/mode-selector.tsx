"use client";

import type { SessionMode } from "@/types/session";

const MODES: { value: SessionMode; label: string; description: string }[] = [
    {
        value: "planning",
        label: "Planning",
        description: "Read-only until you approve a plan — good for exploring before it touches anything.",
    },
    {
        value: "interactive",
        label: "Interactive",
        description: "Asks before every action. Pauses safely if you're not connected to answer.",
    },
    {
        value: "auto",
        label: "Auto accept",
        description: "Keeps working unattended, even while you're offline — for tasks you trust it with.",
    },
];

export function ModeSelector({ value, onChange }: { value: SessionMode; onChange: (mode: SessionMode) => void }) {
    return (
        <div className="space-y-2">
            <label className="text-sm font-medium">Mode</label>
            <div className="grid gap-2">
                {MODES.map((mode) => (
                    <button
                        key={mode.value}
                        type="button"
                        onClick={() => onChange(mode.value)}
                        className={`rounded-card border px-3 py-2 text-left text-sm ${
                            value === mode.value ? "border-accent bg-accent/5" : "border-border"
                        }`}
                    >
                        <div className="font-medium">{mode.label}</div>
                        <div className="text-xs text-muted">{mode.description}</div>
                    </button>
                ))}
            </div>
        </div>
    );
}
