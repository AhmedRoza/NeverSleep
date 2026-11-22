'use strict';
const { app, BrowserWindow, Tray, Menu, ipcMain, powerSaveBlocker, powerMonitor, shell, nativeImage } = require('electron');
const path = require('path');
const store = require('./state');

// Contact links supplied by Ahmed (WhatsApp wa.me/201095064150, Instagram ahmed_roza_).
const CONTACTS = {
  whatsapp: 'https://wa.me/201095064150',
  instagram: 'https://www.instagram.com/ahmed_roza_'
};

let win = null;
let tray = null;
let settings;
let active = false;            // app always launches inactive; user turns it on explicitly
let endsAt = null;             // epoch ms, or null = until stopped
let appBlocker = null;
let dispBlocker = null;
let quitting = false;
let tickTimer = null;

function settingsFile() { return path.join(app.getPath('userData'), 'settings.json'); }
function onBattery() { try { return powerMonitor.isOnBatteryPower(); } catch { return false; } }

function applyBlockers() {
  const want = store.blockersWanted(active, settings, onBattery());
  if (want.app && appBlocker === null) appBlocker = powerSaveBlocker.start('prevent-app-suspension');
  if (!want.app && appBlocker !== null) { powerSaveBlocker.stop(appBlocker); appBlocker = null; }
  if (want.display && dispBlocker === null) dispBlocker = powerSaveBlocker.start('prevent-display-sleep');
  if (!want.display && dispBlocker !== null) { powerSaveBlocker.stop(dispBlocker); dispBlocker = null; }
}

function getState() {
  const want = store.blockersWanted(active, settings, onBattery());
  return {
    active,
    effective: want.app,
    keepScreenOn: settings.keepScreenOn,
    durationMinutes: settings.durationMinutes,
    acOnly: settings.acOnly,
    autoStart: settings.autoStart,
    remainingMs: store.remainingMs(endsAt, Date.now()),
    pausedForBattery: active && settings.acOnly && onBattery()
  };
}

function syncUI() {
  if (win && !win.isDestroyed()) win.webContents.send('state', getState());
  updateTray();
}

function updateTray() {
  if (!tray) return;
  const iconFile = active ? 'tray-on.png' : 'tray-off.png';
  tray.setImage(nativeImage.createFromPath(path.join(__dirname, '..', 'assets', iconFile)));
  let tip = 'Never Sleep - متوقف';
  if (active) {
    const rem = store.remainingMs(endsAt, Date.now());
    tip = rem == null ? 'Never Sleep - شغال (حتى الإيقاف)' : 'Never Sleep - شغال (متبقي ' + fmtRem(rem) + ')';
    const st = getState();
    if (st.pausedForBattery) tip = 'Never Sleep - متوقف مؤقتًا (على البطارية)';
  }
  tray.setToolTip(tip);
  tray.setContextMenu(Menu.buildFromTemplate([
    { label: 'فتح Never Sleep', click: showWindow },
    { label: active ? 'إيقاف منع السكون' : 'تشغيل منع السكون', click: () => { active ? stopActive() : startActive(); } },
    { label: 'المدة', submenu: [0, 60, 120, 240].map((m) => ({
      label: m === 0 ? 'حتى الإيقاف' : (m === 60 ? 'ساعة' : (m === 120 ? 'ساعتين' : '٤ ساعات')),
      type: 'radio',
      checked: settings.durationMinutes === m,
      click: () => { applyDuration(m); }
    })) },
    { label: 'تشغيل تلقائي مع ويندوز', type: 'checkbox', checked: settings.autoStart, click: (item) => { applyAutoStart(item.checked); } },
    { type: 'separator' },
    { label: 'إيقاف وخروج', click: quitApp }
  ]));
}

function fmtRem(ms) {
  const s = Math.ceil(ms / 1000);
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
  return h > 0 ? h + 'س ' + m + 'د' : m + 'د ' + (s % 60) + 'ث';
}

function applyDuration(m) {
  settings.durationMinutes = store.isValidDuration(m) ? m : 0;
  store.saveSettings(settingsFile(), settings);
  if (active) endsAt = store.computeEndsAt(Date.now(), settings.durationMinutes);
  syncUI();
}

function applyAutoStart(v) {
  settings.autoStart = !!v;
  store.saveSettings(settingsFile(), settings);
  try { app.setLoginItemSettings({ openAtLogin: settings.autoStart }); } catch { /* ignore */ }
  syncUI();
}

function startActive() {
  active = true;
  endsAt = store.computeEndsAt(Date.now(), settings.durationMinutes);
  applyBlockers();
  syncUI();
}

function stopActive() {
  active = false;
  endsAt = null;
  applyBlockers();
  syncUI();
}

function quitApp() {
  active = false; endsAt = null;
  applyBlockers(); // stops every blocker, restoring normal Windows sleep behavior
  quitting = true;
  app.quit();
}

function showWindow() {
  if (!win) return;
  if (win.isMinimized()) win.restore();
  win.show();
  win.focus();
}

function createWindow() {
  win = new BrowserWindow({
    width: 420,
    height: 760,
    minWidth: 380,
    minHeight: 700,
    autoHideMenuBar: true,
    backgroundColor: '#faf7f0',
    icon: path.join(__dirname, '..', 'assets', 'icon.png'),
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false
    }
  });
  win.loadFile(path.join(__dirname, 'index.html'));
  win.on('close', (e) => {
    if (!quitting) { e.preventDefault(); win.hide(); }
  });
}

const gotLock = app.requestSingleInstanceLock();
if (!gotLock) {
  app.quit();
} else {
  app.on('second-instance', () => showWindow());
  app.whenReady().then(() => {
    settings = store.loadSettings(settingsFile());
    createWindow();
    const icon = nativeImage.createFromPath(path.join(__dirname, '..', 'assets', 'tray-off.png'));
    try {
      tray = new Tray(icon);
      tray.on('click', () => { win.isVisible() ? win.hide() : showWindow(); });
    } catch { tray = null; }
    powerMonitor.on('on-ac', () => { applyBlockers(); syncUI(); });
    powerMonitor.on('on-battery', () => { applyBlockers(); syncUI(); });
    tickTimer = setInterval(() => {
      if (active && store.isExpired(endsAt, Date.now())) { stopActive(); return; }
      syncUI();
    }, 1000);
    syncUI();
  });
  app.on('window-all-closed', () => { /* keep running in tray */ });
  app.on('before-quit', () => { quitting = true; });
}

ipcMain.handle('get-state', () => getState());
ipcMain.handle('toggle-active', () => { active ? stopActive() : startActive(); });
ipcMain.handle('set-screen-on', (e, v) => { settings.keepScreenOn = !!v; store.saveSettings(settingsFile(), settings); applyBlockers(); syncUI(); });
ipcMain.handle('set-duration', (e, m) => {
  settings.durationMinutes = store.isValidDuration(m) ? m : 0;
  store.saveSettings(settingsFile(), settings);
  if (active) endsAt = store.computeEndsAt(Date.now(), settings.durationMinutes);
  syncUI();
});
ipcMain.handle('set-ac-only', (e, v) => { settings.acOnly = !!v; store.saveSettings(settingsFile(), settings); applyBlockers(); syncUI(); });
ipcMain.handle('set-auto-start', (e, v) => applyAutoStart(v));
ipcMain.handle('minimize-to-tray', () => { if (win) win.hide(); });
ipcMain.handle('stop-exit', () => quitApp());
ipcMain.handle('open-contact', (e, key) => { const url = CONTACTS[key]; if (url) shell.openExternal(url); });
