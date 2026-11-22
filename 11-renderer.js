'use strict';
const $ = (id) => document.getElementById(id);
const mainToggle = $('mainToggle'), screenToggle = $('screenToggle');
const statusText = $('statusText'), countdown = $('countdown'), remaining = $('remaining');
const paused = $('paused'), chips = $('chips'), acOnly = $('acOnly'), autoStart = $('autoStart');
const custH = $('custH'), custM = $('custM');

function fmtRem(ms) {
  const s = Math.max(0, Math.ceil(ms / 1000));
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
  if (h > 0) return h + ' ساعة و ' + m + ' دقيقة';
  if (m > 0) return m + ' دقيقة و ' + sec + ' ثانية';
  return sec + ' ثانية';
}

function render(st) {
  mainToggle.classList.toggle('on', st.active);
  mainToggle.setAttribute('aria-checked', String(st.active));
  screenToggle.classList.toggle('on', st.keepScreenOn);
  screenToggle.setAttribute('aria-checked', String(st.keepScreenOn));
  if (st.pausedForBattery) {
    statusText.textContent = 'متوقف مؤقتًا (على البطارية)';
    statusText.className = 'status-text';
  } else if (st.active) {
    statusText.textContent = st.keepScreenOn ? 'شغال — اللابتوب والشاشة مش هيناموا' : 'شغال — اللابتوب مش هينام';
    statusText.className = 'status-text on';
  } else {
    statusText.textContent = 'متوقف — إعدادات السكون العادية شغالة';
    statusText.className = 'status-text';
  }
  paused.hidden = !st.pausedForBattery;
  if (st.active && st.remainingMs != null) {
    countdown.hidden = false;
    remaining.textContent = fmtRem(st.remainingMs);
  } else {
    countdown.hidden = true;
  }
  chips.querySelectorAll('.chip').forEach((c) => {
    c.classList.toggle('sel', Number(c.dataset.min) === st.durationMinutes);
  });
  // reflect a custom (non-preset) value back into the inputs without fighting typing
  if (![0, 60, 120, 240].includes(st.durationMinutes) && document.activeElement !== custH && document.activeElement !== custM) {
    custH.value = Math.floor(st.durationMinutes / 60);
    custM.value = st.durationMinutes % 60;
  }
  if (acOnly.checked !== st.acOnly) acOnly.checked = st.acOnly;
  if (autoStart.checked !== st.autoStart) autoStart.checked = st.autoStart;
}

mainToggle.addEventListener('click', () => window.ns.toggleActive());
screenToggle.addEventListener('click', () => window.ns.setScreenOn(!screenToggle.classList.contains('on')));
chips.addEventListener('click', (e) => {
  const b = e.target.closest('.chip');
  if (b) window.ns.setDuration(Number(b.dataset.min));
});
acOnly.addEventListener('change', () => window.ns.setAcOnly(acOnly.checked));
autoStart.addEventListener('change', () => window.ns.setAutoStart(autoStart.checked));
$('custApply').addEventListener('click', () => {
  const h = Math.max(0, Math.min(99, parseInt(custH.value, 10) || 0));
  const m = Math.max(0, Math.min(59, parseInt(custM.value, 10) || 0));
  custH.value = h; custM.value = m;
  window.ns.setDuration(h * 60 + m);
});
$('minBtn').addEventListener('click', () => window.ns.minimizeToTray());
$('exitBtn').addEventListener('click', () => window.ns.stopExit());
$('waBtn').addEventListener('click', () => window.ns.openContact('whatsapp'));
$('igBtn').addEventListener('click', () => window.ns.openContact('instagram'));

window.ns.onState(render);
window.ns.getState().then(render);
