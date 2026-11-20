(() => {
'use strict';

/* ===================== Util ===================== */
const $  = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => [...r.querySelectorAll(s)];
const CSRF = $('meta[name="csrf-token"]').content;
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const num = (n, d = 1) => (typeof n === 'number' && isFinite(n)) ? n.toFixed(d) : '–';
const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
const tm = iso => iso ? new Date(iso).toLocaleTimeString('id-ID') : '–';
const dt = iso => iso ? new Date(iso).toLocaleString('id-ID', {day:'2-digit',month:'short',hour:'2-digit',minute:'2-digit',second:'2-digit'}) : '–';

const LABEL = {
  flow:  { IDLE:'Siaga', VALIDASI:'Validasi', MOVING:'Bergerak', VERIFY:'Verifikasi', COMPLETE:'Selesai' },
  lift:  { STOPPED:'Berhenti', UP:'Naik', DOWN:'Turun', BRAKING:'Mengerem', FAULT:'FAULT' },
  alarm: { 'E-STOP':'E-Stop', OVERLOAD:'Beban lebih', TRAVEL_TIMEOUT:'Waktu tempuh', WEIGHT_MISMATCH:'Bobot tak sesuai', DEVICE_OFFLINE:'Perangkat offline', LIFT_FAULT:'Lift FAULT' },
};

function toast(msg, kind = '') {
  const el = document.createElement('div');
  el.className = 'toast ' + kind;
  el.textContent = msg;
  $('#toasts').appendChild(el);
  setTimeout(() => el.remove(), 4200);
}

function addLog(text, cls = '') {
  const ul = $('#log');
  if (ul.firstElementChild?.classList.contains('empty')) ul.innerHTML = '';
  const li = document.createElement('li');
  li.innerHTML = '<span class="t"></span><span></span>';
  li.children[0].textContent = new Date().toLocaleTimeString('id-ID');
  li.children[1].textContent = text;
  if (cls) li.children[1].className = cls;
  ul.prepend(li);
  while (ul.children.length > 60) ul.lastElementChild.remove();
}

/* Semua panggilan lewat Laravel (/proxy/...) — token API tidak pernah ada di browser */
async function api(method, path, body, query) {
  const url = '/proxy/' + path + (query ? '?' + new URLSearchParams(query) : '');
  try {
    const res = await fetch(url, {
      method,
      headers: { 'Accept':'application/json', 'Content-Type':'application/json', 'X-CSRF-TOKEN': CSRF },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
    const j = await res.json().catch(() => null);
    if (!j) return { ok:false, error:{ code:'BAD_RESPONSE', message:'Respons server tidak valid' } };
    if (j.error) return { ok:false, error:j.error };
    return { ok:true, data:j.data };
  } catch {
    return { ok:false, error:{ code:'NET', message:'Tidak dapat menghubungi server Laravel' } };
  }
}

/* Kirim perintah kontrol + log + toast */
async function act(label, method, path, body) {
  addLog('Dikirim: ' + label);
  const r = await api(method, path, body);
  if (r.ok) { addLog('Berhasil: ' + label, 'okk'); toast(label + ' terkirim', 'ok'); }
  else      { addLog('Ditolak: ' + label + '. ' + r.error.message, 'rej'); toast(r.error.message, 'bad'); }
  pollStatus();
  return r;
}

/* ===================== Navigasi ===================== */
const loaders = { items: loadItems, moves: loadMoves, alarms: loadAlarms, settings: loadSettings };
function show(view) {
  $$('.nav button').forEach(b => b.classList.toggle('on', b.dataset.view === view));
  $$('.view').forEach(v => v.classList.toggle('on', v.id === 'view-' + view));
  if (loaders[view]) loaders[view]();
  history.replaceState(null, '', '#' + view);
}
$$('.nav button').forEach(b => b.onclick = () => show(b.dataset.view));

/* ===================== Status lift ===================== */
let S = null;            // status terakhir
let LEVELS = [];         // kapasitas rak + jarak referensi
let MAX_LOAD = 800;
let backendUp = false;

function setLink(id, ok, text) {
  const el = $(id);
  el.className = 'link ' + (ok ? 'ok' : 'bad');
  el.lastElementChild.textContent = text;
}
function pressed(id, on) { $(id).setAttribute('aria-pressed', on ? 'true' : 'false'); }

function placeCar(d) {
  const shaft = $('#shaft'), car = $('#car');
  const H = shaft.clientHeight;
  const top2 = 4, top1 = H - 106;
  let pct = d.current_level === 2 ? 0 : 1;

  const moving = ['UP','DOWN','BRAKING'].includes(d.lift_status);
  const l1 = LEVELS.find(l => l.level === 1), l2 = LEVELS.find(l => l.level === 2);
  if (moving && l1 && l2 && l1.ref_distance_cm !== l2.ref_distance_cm && d.distance_cm > 0 && d.distance_cm < 400) {
    pct = clamp((d.distance_cm - l2.ref_distance_cm) / (l1.ref_distance_cm - l2.ref_distance_cm), 0, 1);
  }
  car.style.top = (top2 + pct * (top1 - top2)) + 'px';
  car.className = 'car' + (d.lift_status === 'FAULT' || d.estop ? ' fault' : (moving || d.flow_state === 'MOVING' ? ' moving' : ''));
}

function renderStatus(d) {
  S = d;
  const online = d.status === 'online';
  setLink('#lnkDevice', online, `${d.device_id || 'Perangkat'}: ${online ? 'online' : 'offline'}`);

  $('#chMode').textContent   = 'Mode: ' + (d.mode === 'AUTO' ? 'Otomatis' : d.mode === 'MANUAL' ? 'Manual' : '–');
  $('#chDriver').textContent = 'Driver: ' + (d.enabled ? 'Aktif' : 'Mati');
  $('#chDriver').className   = 'chip' + (d.enabled ? ' on' : '');
  $('#chSeen').textContent   = 'Terakhir aktif: ' + tm(d.last_seen);

  $('#vLevel').textContent  = d.current_level ? 'Lantai ' + d.current_level : '–';
  $('#vLift').textContent   = LABEL.lift[d.lift_status] || d.lift_status || '–';
  $('#vFlow').textContent   = LABEL.flow[d.flow_state] || d.flow_state || '–';
  $('#vDist').textContent   = num(d.distance_cm) + ' cm';
  $('#vTravel').textContent = num(d.travel_time_sec) + ' dtk';
  $('#readout').textContent = d.active_command_id ? 'Perintah: ' + d.active_command_id : '';

  const w = d.weight_g ?? 0, pct = clamp(w / MAX_LOAD * 100, 0, 100);
  $('#vWeight').textContent = num(w);
  $('#vMax').textContent = MAX_LOAD;
  $('#vWeightPct').textContent = Math.round(w / MAX_LOAD * 100) + '%';
  const bar = $('#wBar');
  bar.style.width = pct + '%';
  bar.className = w > MAX_LOAD ? 'over' : (pct > 80 ? 'hot' : '');
  $('#cargo').className = 'cargo' + (w > 20 ? ' on' : '');

  pressed('#mAuto', d.mode === 'AUTO');  pressed('#mMan', d.mode === 'MANUAL');
  pressed('#dOn', !!d.enabled);          pressed('#dOff', !d.enabled);

  // Tombol yang tidak relevan di mode saat ini dinonaktifkan agar tidak membingungkan
  const manual = d.mode === 'MANUAL';
  ['#bMove1','#bMove2'].forEach(s => $(s).disabled = !manual);
  $('#bQr').disabled = d.mode !== 'AUTO';

  placeCar(d);

  // Banner peringatan berdasarkan prioritas
  const a = $('#alert');
  let msg = '', blink = false;
  if (d.estop) { msg = 'Emergency stop aktif. Motor berhenti. Lepas tombol E-stop fisik, lalu tekan Reset FAULT.'; blink = true; }
  else if (d.lift_status === 'FAULT') { msg = 'Lift dalam status FAULT. Periksa penyebabnya, lalu tekan Reset FAULT.'; blink = true; }
  else if (!online) { msg = 'Perangkat ESP32 offline. Perintah tidak akan diterima sampai perangkat terhubung kembali.'; }
  a.hidden = !msg; a.textContent = msg; a.classList.toggle('blink', blink);
}

async function pollStatus() {
  const r = await api('GET', 'lift/status');
  backendUp = r.ok;
  setLink('#lnkBackend', r.ok, r.ok ? 'Backend: terhubung' : 'Backend: terputus');
  if (r.ok) renderStatus(r.data);
  else {
    const a = $('#alert');
    a.hidden = false; a.classList.remove('blink');
    a.textContent = r.error.message;
  }
}

async function pollLevels() {
  const r = await api('GET', 'stock/levels');
  if (!r.ok) return;
  LEVELS = r.data || [];
  [1, 2].forEach(n => {
    const l = LEVELS.find(x => x.level === n);
    const box = $('#slots' + n);
    box.innerHTML = '';
    if (!l) return;
    for (let i = 0; i < l.capacity; i++) {
      const s = document.createElement('div');
      s.className = 'slot' + (i < l.occupied_count ? ' full' : '');
      box.appendChild(s);
    }
  });
  $('#levels').innerHTML = LEVELS.map(l => `
    <div>
      <div class="lv-top"><span>Lantai ${l.level} <small>${l.is_input_area ? '· area input' : '· rak penyimpanan'}</small></span>
        <span>${l.occupied_count}/${l.capacity}</span></div>
      <div class="lv-bar">${Array.from({length:l.capacity},(_,i)=>`<i class="${i<l.occupied_count?'f':''}"></i>`).join('')}</div>
    </div>`).join('') || '<p class="muted">Belum ada data lantai.</p>';
}

async function pollAlarmBadge() {
  const r = await api('GET', 'alarms', undefined, { unack: 'true' });
  if (!r.ok) return;
  const n = (r.data || []).length, b = $('#alarmBadge');
  b.hidden = n === 0; b.textContent = n;
}

async function loadMaxLoad() {
  const r = await api('GET', 'settings');
  if (r.ok && r.data?.max_load_g) { const v = parseFloat(r.data.max_load_g); if (v > 0) MAX_LOAD = v; }
}

/* ===================== Kendali ===================== */
$('#bStop').onclick  = () => act('Emergency stop', 'POST', 'lift/stop');
$('#bReset').onclick = () => act('Reset FAULT', 'POST', 'lift/reset');
$('#bHome').onclick  = () => act('Home ke Lantai 1', 'POST', 'lift/home');
$('#mAuto').onclick  = () => act('Mode otomatis', 'POST', 'lift/mode', { mode: 'AUTO' });
$('#mMan').onclick   = () => act('Mode manual', 'POST', 'lift/mode', { mode: 'MANUAL' });
$('#dOn').onclick    = () => act('Aktifkan driver', 'POST', 'lift/enable');
$('#dOff').onclick   = () => act('Matikan driver', 'POST', 'lift/disable');
$('#bMove2').onclick = () => act('Pindah ke Lantai 2', 'POST', 'lift/move', { target_level: 2 });
$('#bMove1').onclick = () => act('Pindah ke Lantai 1', 'POST', 'lift/move', { target_level: 1 });
$('#bJogUp').onclick   = () => act('Jog naik', 'POST', 'lift/jog', { direction: 'UP' });
$('#bJogDown').onclick = () => act('Jog turun', 'POST', 'lift/jog', { direction: 'DOWN' });

/* ===================== Transaksi QR ===================== */
let qrAction = 'STORE', trackTimer = null;
function setAction(a) {
  qrAction = a;
  pressed('#aStore', a === 'STORE'); pressed('#aRetr', a === 'RETRIEVE');
  $('#qrLevel').style.display = a === 'STORE' ? '' : 'none';
}
$('#aStore').onclick = () => setAction('STORE');
$('#aRetr').onclick  = () => setAction('RETRIEVE');
setAction('STORE');

const STEPS = ['PENDING', 'MOVING', 'VERIFYING', 'COMPLETE'];
function renderTrack(cmd) {
  $('#track').hidden = false;
  $('#trkId').textContent = cmd.command_id;
  const st = $('#trkStatus'); st.textContent = cmd.status; st.className = 'tag ' + cmd.status;
  const idx = STEPS.indexOf(cmd.status);
  $$('#trkSteps li').forEach((li, i) => {
    li.className = cmd.status === 'FAILED' ? (i === 0 ? 'done' : '') : (i < idx || cmd.status === 'COMPLETE' ? 'done' : i === idx ? 'cur' : '');
  });
  $('#trkNote').textContent = cmd.status === 'FAILED' ? ('Gagal: ' + (cmd.fault_reason || 'alasan tidak diketahui')) : '';
  if (cmd.status === 'FAILED') $('#trkSteps li:last-child').className = 'fail';
}
function track(id) {
  clearInterval(trackTimer);
  let n = 0;
  const tick = async () => {
    const r = await api('GET', 'commands/' + encodeURIComponent(id));
    if (r.ok) {
      renderTrack(r.data);
      if (['COMPLETE', 'FAILED'].includes(r.data.status)) {
        clearInterval(trackTimer);
        addLog((r.data.status === 'COMPLETE' ? 'Selesai: ' : 'Gagal: ') + id, r.data.status === 'COMPLETE' ? 'okk' : 'rej');
        pollLevels(); pollAlarmBadge();
      }
    }
    if (++n > 150) clearInterval(trackTimer);
  };
  tick(); trackTimer = setInterval(tick, 1200);
}
$('#bQr').onclick = async () => {
  const qr = $('#qrCode').value.trim();
  if (!qr) { toast('Isi kode QR terlebih dahulu', 'bad'); return; }
  const body = { qr, action: qrAction };
  if (qrAction === 'STORE') body.target_level = parseInt($('#qrLevel').value, 10);
  addLog(`Dikirim: ${qrAction === 'STORE' ? 'Simpan' : 'Ambil'} ${qr}`);
  const r = await api('POST', 'commands', body);
  if (!r.ok) { addLog('Ditolak: ' + r.error.message, 'rej'); toast(r.error.message, 'bad'); return; }
  toast(`${r.data.item_name} → perintah ${r.data.command_id}`, 'ok');
  track(r.data.command_id);
  pollStatus();
};

/* ===================== Barang & QR ===================== */
let ITEMS = [];
async function loadItems() {
  const r = await api('GET', 'items');
  const tb = $('#tblItems tbody');
  if (!r.ok) { tb.innerHTML = `<tr><td colspan="6" class="muted">${esc(r.error.message)}</td></tr>`; return; }
  ITEMS = r.data || [];
  if (!ITEMS.length) { tb.innerHTML = '<tr><td colspan="6" class="muted">Belum ada barang. Tekan “Barang baru”.</td></tr>'; return; }
  tb.innerHTML = ITEMS.map(it => {
    const stock = it.current_stock || 0, low = stock < it.min_stock;
    const pct = it.min_stock > 0 ? clamp(stock / (it.min_stock * 2) * 100, 4, 100) : 100;
    return `<tr>
      <td><code>${esc(it.sku)}</code></td>
      <td>${esc(it.name)}</td>
      <td>${esc(it.category) || '<span class="muted">–</span>'}</td>
      <td class="r">${num(it.expected_weight_g)} g ±${num(it.weight_tolerance_pct, 0)}%</td>
      <td><div class="stock ${low ? 'low' : ''}"><span>${stock}</span><span class="mini"><i style="width:${pct}%"></i></span><small>min ${it.min_stock}</small></div></td>
      <td class="r"><button data-gen="${it.id}">Buat unit &amp; QR</button></td></tr>`;
  }).join('');
  $$('[data-gen]', tb).forEach(b => b.onclick = () => genUnits(parseInt(b.dataset.gen, 10)));
}

async function genUnits(id) {
  const it = ITEMS.find(x => x.id === id); if (!it) return;
  const count = parseInt(prompt(`Berapa unit untuk ${it.sku}? (maks 100)`, '4'), 10);
  if (!count || count < 1) return;
  const start = parseInt(prompt('Nomor serial awal (isi nomor setelah serial terakhir yang pernah dibuat):', '1'), 10) || 1;
  const r = await api('POST', `items/${id}/units`, { count, start_serial: start });
  if (!r.ok) { toast(r.error.message, 'bad'); return; }
  const units = r.data || [];
  if (!units.length) { toast('Tidak ada unit baru. Serial itu sudah terpakai — naikkan nomor serial awal.', 'bad'); return; }
  toast(units.length + ' unit dibuat', 'ok');
  $('#unitsCard').hidden = false;
  $('#unitsTitle').textContent = `Label QR · ${it.name}`;
  $('#qrGrid').innerHTML = units.map(u => `<div class="qr-tile">${qrSvg(u.qr_code)}<b>${esc(u.qr_code)}</b><small>${esc(it.name)}</small></div>`).join('');
  $('#unitsCard').scrollIntoView({ behavior: 'smooth', block: 'start' });
  loadItems();
}
function qrSvg(text) {
  if (typeof qrcode !== 'function') return '<div style="padding:30px 0;color:#777;font-size:12px">Pustaka QR tidak termuat (perlu internet)</div>';
  const q = qrcode(0, 'M'); q.addData(text); q.make();
  return q.createSvgTag({ cellSize: 4, margin: 2, scalable: true });
}
$('#bPrint').onclick = () => window.print();

const dlg = $('#dlgItem');
$('#bNewItem').onclick = () => { $('#frmItem').reset(); dlg.showModal(); };
$('#dlgCancel').onclick = () => dlg.close();
$('#frmItem').onsubmit = async e => {
  e.preventDefault();
  const f = new FormData(e.target);
  const body = {
    sku: f.get('sku').trim(), name: f.get('name').trim(), category: f.get('category').trim(),
    expected_weight_g: parseFloat(f.get('expected_weight_g')) || 0,
    weight_tolerance_pct: parseFloat(f.get('weight_tolerance_pct')) || 10,
    min_stock: parseInt(f.get('min_stock'), 10) || 0,
  };
  const r = await api('POST', 'items', body);
  if (!r.ok) { toast(r.error.message, 'bad'); return; }
  dlg.close(); toast('Barang ditambahkan', 'ok'); loadItems();
};

/* ===================== Riwayat ===================== */
const MV = { offset: 0, limit: 15, total: 0 };
async function loadMoves() {
  const q = { limit: MV.limit, offset: MV.offset };
  const t = $('#mvType').value, s = $('#mvSearch').value.trim();
  if (t) q.type = t; if (s) q.search = s;
  const r = await api('GET', 'movements', undefined, q);
  const tb = $('#tblMoves tbody');
  if (!r.ok) { tb.innerHTML = `<tr><td colspan="8" class="muted">${esc(r.error.message)}</td></tr>`; return; }
  const list = r.data.items || []; MV.total = r.data.total || 0;
  tb.innerHTML = list.length ? list.map(m => `<tr>
      <td>${dt(m.created_at)}</td>
      <td><span class="tag ${m.type === 'IN' ? 'COMPLETE' : ''}">${esc(m.type)}</span></td>
      <td>${m.sku ? `<code>${esc(m.sku)}</code>` : '–'}</td><td>${esc(m.serial) || '–'}</td>
      <td>${m.level ?? '–'}</td><td class="r">${m.weight_g != null ? num(m.weight_g) + ' g' : '–'}</td>
      <td><span class="tag ${esc(m.status)}">${esc(m.status)}</span></td><td>${esc(m.note) || ''}</td></tr>`).join('')
    : '<tr><td colspan="8" class="muted">Belum ada pergerakan.</td></tr>';
  const from = MV.total ? MV.offset + 1 : 0, to = Math.min(MV.total, MV.offset + MV.limit);
  $('#mvInfo').textContent = `${from}–${to} dari ${MV.total}`;
  $('#mvPrev').disabled = MV.offset === 0; $('#mvNext').disabled = to >= MV.total;
}
$('#mvPrev').onclick = () => { MV.offset = Math.max(0, MV.offset - MV.limit); loadMoves(); };
$('#mvNext').onclick = () => { MV.offset += MV.limit; loadMoves(); };
$('#mvType').onchange = () => { MV.offset = 0; loadMoves(); };
let mvT; $('#mvSearch').oninput = () => { clearTimeout(mvT); mvT = setTimeout(() => { MV.offset = 0; loadMoves(); }, 350); };

/* ===================== Alarm ===================== */
let alarmOnlyUnack = true;
async function loadAlarms() {
  const r = await api('GET', 'alarms', undefined, alarmOnlyUnack ? { unack: 'true' } : undefined);
  const box = $('#alarmList');
  if (!r.ok) { box.innerHTML = `<div class="empty-state">${esc(r.error.message)}</div>`; return; }
  const list = r.data || [];
  box.innerHTML = list.length ? list.map(a => `
    <div class="alarm ${a.is_acknowledged ? 'ack' : ''}">
      <span class="ty">${esc(LABEL.alarm[a.type] || a.type)}</span>
      <div><p>${esc(a.message)}</p><small>${dt(a.created_at)}${a.command_id ? ' · ' + esc(a.command_id) : ''}${a.acknowledged_by ? ' · diakui ' + esc(a.acknowledged_by) : ''}</small></div>
      ${a.is_acknowledged ? '<span class="tag COMPLETE">Diakui</span>' : `<button data-ack="${a.id}">Akui</button>`}
    </div>`).join('') : '<div class="empty-state">Tidak ada alarm. Semua tenang.</div>';
  $$('[data-ack]', box).forEach(b => b.onclick = async () => {
    const r2 = await api('PATCH', `alarms/${b.dataset.ack}/ack`, { user: 'Operator Dashboard' });
    if (r2.ok) { toast('Alarm diakui', 'ok'); loadAlarms(); pollAlarmBadge(); } else toast(r2.error.message, 'bad');
  });
}
$('#alUnack').onclick = () => { alarmOnlyUnack = true;  pressed('#alUnack', true);  pressed('#alAll', false); loadAlarms(); };
$('#alAll').onclick   = () => { alarmOnlyUnack = false; pressed('#alUnack', false); pressed('#alAll', true);  loadAlarms(); };

/* ===================== Pengaturan ===================== */
async function loadSettings() {
  const r = await api('GET', 'settings');
  const box = $('#settingsList');
  if (!r.ok) { box.innerHTML = `<p class="muted">${esc(r.error.message)}</p>`; return; }
  const keys = Object.keys(r.data || {}).sort();
  box.innerHTML = keys.length ? keys.map(k => `<div class="set-row"><b>${esc(k)}</b><input data-key="${esc(k)}" value="${esc(r.data[k])}"></div>`).join('')
                              : '<p class="muted">Belum ada pengaturan.</p>';
}
$('#bSaveSettings').onclick = async () => {
  const body = {}; $$('#settingsList input').forEach(i => body[i.dataset.key] = i.value);
  const r = await api('PUT', 'settings', body);
  if (r.ok) { toast('Pengaturan disimpan', 'ok'); loadMaxLoad(); } else toast(r.error.message, 'bad');
};

/* ===================== Mulai ===================== */
window.addEventListener('resize', () => S && placeCar(S));
loadMaxLoad().then(pollLevels).then(pollStatus).then(pollAlarmBadge);
setInterval(pollStatus, 1500);
setInterval(pollLevels, 5000);
setInterval(pollAlarmBadge, 6000);
const first = location.hash.slice(1);
if (first && $('#view-' + first)) show(first);
})();
