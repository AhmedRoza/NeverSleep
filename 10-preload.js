'use strict';
const { contextBridge, ipcRenderer } = require('electron');
contextBridge.exposeInMainWorld('ns', {
  getState: () => ipcRenderer.invoke('get-state'),
  toggleActive: () => ipcRenderer.invoke('toggle-active'),
  setScreenOn: (v) => ipcRenderer.invoke('set-screen-on', v),
  setDuration: (m) => ipcRenderer.invoke('set-duration', m),
  setAcOnly: (v) => ipcRenderer.invoke('set-ac-only', v),
  setAutoStart: (v) => ipcRenderer.invoke('set-auto-start', v),
  minimizeToTray: () => ipcRenderer.invoke('minimize-to-tray'),
  stopExit: () => ipcRenderer.invoke('stop-exit'),
  openContact: (k) => ipcRenderer.invoke('open-contact', k),
  onState: (cb) => ipcRenderer.on('state', (e, s) => cb(s))
});
