'use strict';
const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const s = require('../app/state');

let pass = 0;
function t(name, fn) { fn(); pass++; console.log('ok - ' + name); }

t('defaults: inactive choices (screen off, until-stopped, not AC-only)', () => {
  assert.deepStrictEqual(s.DEFAULTS, { keepScreenOn: false, durationMinutes: 0, acOnly: false, autoStart: false });
});

t('sanitize keeps only valid values; custom minutes allowed within cap', () => {
  assert.deepStrictEqual(s.sanitize({ keepScreenOn: true, durationMinutes: -5, acOnly: 1 }),
    { keepScreenOn: true, durationMinutes: 0, acOnly: false, autoStart: false });
  assert.strictEqual(s.sanitize({ autoStart: true }).autoStart, true);
  assert.strictEqual(s.sanitize({ autoStart: 1 }).autoStart, false);
  assert.strictEqual(s.sanitize({ durationMinutes: 45 }).durationMinutes, 45);
  assert.strictEqual(s.sanitize({ durationMinutes: 135 }).durationMinutes, 135);
  assert.strictEqual(s.sanitize({ durationMinutes: 6000 }).durationMinutes, 6000);
  assert.strictEqual(s.sanitize({ durationMinutes: 6001 }).durationMinutes, 0);
  assert.strictEqual(s.sanitize({ durationMinutes: 1.5 }).durationMinutes, 0);
});

t('isValidDuration bounds', () => {
  assert.strictEqual(s.isValidDuration(0), true);
  assert.strictEqual(s.isValidDuration(240), true);
  assert.strictEqual(s.isValidDuration(90), true);
  assert.strictEqual(s.isValidDuration(-1), false);
  assert.strictEqual(s.isValidDuration('60'), false);
  assert.strictEqual(s.isValidDuration(6001), false);
});

t('save/load round-trip preserves custom duration', () => {
  const f = path.join(os.tmpdir(), 'ns-test-' + Date.now() + '.json');
  s.saveSettings(f, { keepScreenOn: true, durationMinutes: 187, acOnly: true, autoStart: true });
  assert.deepStrictEqual(s.loadSettings(f), { keepScreenOn: true, durationMinutes: 187, acOnly: true, autoStart: true });
  fs.unlinkSync(f);
});

t('missing/corrupt settings file falls back to defaults', () => {
  const f = path.join(os.tmpdir(), 'ns-test-missing.json');
  assert.deepStrictEqual(s.loadSettings(f), s.DEFAULTS);
  fs.writeFileSync(f, 'not json{');
  assert.deepStrictEqual(s.loadSettings(f), s.DEFAULTS);
  fs.unlinkSync(f);
});

t('blockersWanted: inactive = nothing', () => {
  assert.deepStrictEqual(s.blockersWanted(false, { keepScreenOn: true, acOnly: false }, false), { app: false, display: false });
});

t('blockersWanted: active, screen on', () => {
  assert.deepStrictEqual(s.blockersWanted(true, { keepScreenOn: true, acOnly: false }, false), { app: true, display: true });
});

t('blockersWanted: active, screen off keeps app block only', () => {
  assert.deepStrictEqual(s.blockersWanted(true, { keepScreenOn: false, acOnly: false }, false), { app: true, display: false });
});

t('blockersWanted: AC-only pauses everything on battery, resumes on AC', () => {
  assert.deepStrictEqual(s.blockersWanted(true, { keepScreenOn: true, acOnly: true }, true), { app: false, display: false });
  assert.deepStrictEqual(s.blockersWanted(true, { keepScreenOn: true, acOnly: true }, false), { app: true, display: true });
});

t('blockersWanted: AC-only ignored when off', () => {
  assert.deepStrictEqual(s.blockersWanted(true, { keepScreenOn: true, acOnly: false }, true), { app: true, display: true });
});

t('endsAt: 0 = until stopped, otherwise now + minutes (custom too)', () => {
  assert.strictEqual(s.computeEndsAt(1000, 0), null);
  assert.strictEqual(s.computeEndsAt(1000, 60), 1000 + 3600000);
  assert.strictEqual(s.computeEndsAt(1000, 187), 1000 + 187 * 60000);
});

t('remaining + expiry', () => {
  const end = 100000;
  assert.strictEqual(s.remainingMs(end, 40000), 60000);
  assert.strictEqual(s.remainingMs(end, 200000), 0);
  assert.strictEqual(s.remainingMs(null, 5), null);
  assert.strictEqual(s.isExpired(end, 99999), false);
  assert.strictEqual(s.isExpired(end, 100000), true);
  assert.strictEqual(s.isExpired(null, 99999999), false);
});

console.log(pass + ' tests passed');
