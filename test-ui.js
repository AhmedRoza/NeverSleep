'use strict';
const { app, BrowserWindow } = require('electron');
const path = require('path');
const fs = require('fs');
app.commandLine.appendSwitch('no-sandbox');
app.whenReady().then(() => {
  const win = new BrowserWindow({ width: 420, height: 780, show: false, backgroundColor: '#faf7f0', webPreferences: { preload: path.join(__dirname, 'test-preload.js') } });
  win.loadFile(path.join(__dirname, 'app', 'index.html'));
  setTimeout(async () => {
    const img = await win.webContents.capturePage();
    fs.writeFileSync('/tmp/ui-active.png', img.toPNG());
    app.quit();
  }, 3000);
});
