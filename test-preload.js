'use strict';
const { contextBridge } = require('electron');
const state = { active: true, effective: true, keepScreenOn: true, durationMinutes: 187, acOnly: true, remainingMs: 112 * 60 * 1000, pausedForBattery: false };
contextBridge.exposeInMainWorld('ns', {
  getState: async () => state,
  toggleActive: async () => { state.active = !state.active; window.__render && window.__render(state); },
  setScreenOn: async () => {}, setDuration: async () => {}, setAcOnly: async () => {},
  minimizeToTray: async () => {}, stopExit: async () => {}, openContact: async () => {},
  onState: (cb) => { window.__render = cb; cb(state); }
});
