'use strict';
const fs = require('fs');

const DEFAULTS = { keepScreenOn: false, durationMinutes: 0, acOnly: false, autoStart: false };
const PRESETS = [0, 60, 120, 240]; // quick-pick chips; 0 = until stopped
const MAX_DURATION_MIN = 6000; // custom durations capped at 100 hours

function isValidDuration(m) {
  return Number.isInteger(m) && m >= 0 && m <= MAX_DURATION_MIN;
}

function sanitize(raw) {
  const out = { ...DEFAULTS };
  if (raw && typeof raw === 'object') {
    if (typeof raw.keepScreenOn === 'boolean') out.keepScreenOn = raw.keepScreenOn;
    if (isValidDuration(raw.durationMinutes)) out.durationMinutes = raw.durationMinutes;
    if (typeof raw.acOnly === 'boolean') out.acOnly = raw.acOnly;
    if (typeof raw.autoStart === 'boolean') out.autoStart = raw.autoStart;
  }
  return out;
}

function loadSettings(file) {
  try { return sanitize(JSON.parse(fs.readFileSync(file, 'utf8'))); }
  catch { return { ...DEFAULTS }; }
}

function saveSettings(file, s) {
  try { fs.writeFileSync(file, JSON.stringify(sanitize(s), null, 2)); } catch { /* ignore */ }
}

// Which blockers should be running, given activation + battery state.
// AC-only pauses blocking on battery without clearing the user's activation.
function blockersWanted(active, settings, onBattery) {
  const eff = active && !(settings.acOnly && onBattery);
  return { app: eff, display: eff && settings.keepScreenOn };
}

function remainingMs(endsAt, now) {
  return endsAt == null ? null : Math.max(0, endsAt - now);
}

function isExpired(endsAt, now) {
  return endsAt != null && now >= endsAt;
}

function computeEndsAt(now, durationMinutes) {
  return durationMinutes > 0 ? now + durationMinutes * 60000 : null;
}

module.exports = { DEFAULTS, PRESETS, MAX_DURATION_MIN, isValidDuration, sanitize, loadSettings, saveSettings, blockersWanted, remainingMs, isExpired, computeEndsAt };
