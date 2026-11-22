'use strict';
const fs = require('fs');
const PELibrary = require('pe-library');
const ResEdit = require('resedit');

const exePath = 'dist/win-unpacked/Never Sleep.exe';
const data = fs.readFileSync(exePath);
const exe = PELibrary.NtExecutable.from(data);
const res = PELibrary.NtExecutableResource.from(exe);

const iconFile = ResEdit.Data.IconFile.from(fs.readFileSync('assets/icon.ico'));
// Replace the exe's existing icon groups (Electron ships group id 1); adding a new
// id leaves the original atom icon as the one Explorer/taskbar actually shows.
const existing = ResEdit.Resource.IconGroupEntry.fromEntries(res.entries).map((g) => g.id);
for (const id of existing) {
  ResEdit.Resource.IconGroupEntry.replaceIconsForResource(res.entries, id, 1033, iconFile.icons.map((i) => i.data));
}

const vi = ResEdit.Resource.VersionInfo.fromEntries(res.entries)[0];
vi.setFileVersion(1, 3, 0, 0, 1033);
vi.setStringValues({ lang: 1033, codepage: 1200 }, {
  FileDescription: 'Never Sleep - keeps your laptop awake',
  ProductName: 'Never Sleep',
  CompanyName: 'Ahmed Roza',
  LegalCopyright: 'Ahmed Roza'
});
vi.outputToResourceEntries(res.entries);
res.outputResource(exe);
fs.writeFileSync(exePath, Buffer.from(exe.generate()));
console.log('stamped ok');
