@extends('layouts.app')

@section('title', 'Smart Warehouse · ' . $device)

@section('content')
<div class="app">

  {{-- ================= SIDEBAR ================= --}}
  <aside class="side" aria-label="Navigasi utama">
    <div class="brand">
      <svg viewBox="0 0 40 40" width="38" height="38" aria-hidden="true">
        <rect x="3" y="3" width="34" height="34" rx="9" fill="#F26B1D"/>
        <path d="M13 11v18M27 11v18" stroke="#10303A" stroke-width="3" stroke-linecap="round"/>
        <rect x="11" y="15" width="18" height="5" rx="1.5" fill="#10303A"/>
        <rect x="16" y="22" width="8" height="6" rx="1" fill="#fff"/>
      </svg>
      <div>
        <strong>Smart Warehouse</strong>
        <span>Pusat kendali lift</span>
      </div>
    </div>

    <nav class="nav" id="nav">
      <button data-view="dash" class="on">
        <svg viewBox="0 0 24 24"><path d="M4 13h6V4H4zm0 7h6v-5H4zm10 0h6V11h-6zm0-16v5h6V4z"/></svg>Dasbor
      </button>
      <button data-view="items">
        <svg viewBox="0 0 24 24"><path d="M3 7l9-4 9 4v10l-9 4-9-4zm9 4v10M3 7l9 4 9-4"/></svg>Barang &amp; QR
      </button>
      <button data-view="moves">
        <svg viewBox="0 0 24 24"><path d="M4 7h13m0 0-3-3m3 3-3 3M20 17H7m0 0 3-3m-3 3 3 3"/></svg>Riwayat
      </button>
      <button data-view="alarms">
        <svg viewBox="0 0 24 24"><path d="M12 4a6 6 0 0 0-6 6v4l-2 3h16l-2-3v-4a6 6 0 0 0-6-6zm-2 14a2 2 0 0 0 4 0"/></svg>Alarm
        <b class="badge" id="alarmBadge" hidden>0</b>
      </button>
      <button data-view="settings">
        <svg viewBox="0 0 24 24"><path d="M5 7h9m4 0h1M5 17h1m4 0h9M14 7a2 2 0 1 0 4 0 2 2 0 0 0-4 0zM6 17a2 2 0 1 0 4 0 2 2 0 0 0-4 0z"/></svg>Pengaturan
      </button>
    </nav>

    <div class="side-foot">
      <div class="link" id="lnkBackend"><i class="dot"></i><span>Backend: memeriksa…</span></div>
      <div class="link" id="lnkDevice"><i class="dot"></i><span>{{ $device }}: memeriksa…</span></div>
    </div>
  </aside>

  {{-- ================= KONTEN ================= --}}
  <main class="stage">

    <div class="alert" id="alert" role="alert" hidden></div>

    {{-- ---------- DASBOR ---------- --}}
    <section class="view on" id="view-dash">
      <header class="head">
        <div>
          <p class="kicker">Ruang kendali</p>
          <h1>Lift Gudang <em>{{ $device }}</em></h1>
        </div>
        <div class="chips">
          <span class="chip" id="chMode">Mode: –</span>
          <span class="chip" id="chDriver">Driver: –</span>
          <span class="chip" id="chSeen">Terakhir aktif: –</span>
        </div>
      </header>

      <div class="dash">
        {{-- Poros lift: elemen utama --}}
        <div class="card shaft-card">
          <div class="shaft" id="shaft">
            <div class="rail l"></div><div class="rail r"></div>
            <div class="floor" id="floor2"><span>Lantai 2 · Rak</span><div class="slots" id="slots2"></div></div>
            <div class="floor" id="floor1"><span>Lantai 1 · Input</span><div class="slots" id="slots1"></div></div>
            <div class="car" id="car">
              <div class="cargo" id="cargo"></div>
              <div class="deck"></div>
            </div>
            <div class="readout" id="readout">–</div>
          </div>
          <div class="shaft-foot">
            <div><small>Lantai</small><b id="vLevel">–</b></div>
            <div><small>Status lift</small><b id="vLift">–</b></div>
            <div><small>Alur</small><b id="vFlow">–</b></div>
            <div><small>Jarak ultrasonik</small><b id="vDist">–</b></div>
            <div><small>Waktu tempuh</small><b id="vTravel">–</b></div>
          </div>
        </div>

        <div class="col">
          <div class="card">
            <h2>Beban di platform</h2>
            <div class="weight"><span id="vWeight">0</span><small>gram</small></div>
            <div class="meter"><i id="wBar"></i><u id="wMark"></u></div>
            <div class="meter-cap"><span id="vWeightPct">0%</span><span>batas <b id="vMax">800</b> g</span></div>
          </div>

          <div class="card">
            <h2>Kapasitas rak</h2>
            <div id="levels" class="levels"><p class="muted">Memuat…</p></div>
          </div>
        </div>

        {{-- Kendali --}}
        <div class="card ctrl">
          <h2>Kendali</h2>
          <div class="ctrl-top">
            <button class="estop" id="bStop"><span>E-STOP</span><small>Hentikan semua gerakan</small></button>
            <div class="ctrl-grid">
              <div class="field">
                <label>Mode</label>
                <div class="seg" role="group" aria-label="Mode operasi">
                  <button id="mAuto" aria-pressed="false">Otomatis</button>
                  <button id="mMan" aria-pressed="false">Manual</button>
                </div>
              </div>
              <div class="field">
                <label>Driver motor</label>
                <div class="seg" role="group" aria-label="Driver motor">
                  <button id="dOn" aria-pressed="false">Aktif</button>
                  <button id="dOff" aria-pressed="false">Mati</button>
                </div>
              </div>
              <div class="field">
                <label>Pindah (mode manual)</label>
                <div class="btnrow">
                  <button class="solid" id="bMove2">▲ Lantai 2</button>
                  <button class="solid" id="bMove1">▼ Lantai 1</button>
                </div>
              </div>
              <div class="field">
                <label>Jog &amp; kalibrasi</label>
                <div class="btnrow">
                  <button id="bJogUp">Jog naik</button>
                  <button id="bJogDown">Jog turun</button>
                  <button id="bHome">Home</button>
                  <button id="bReset">Reset FAULT</button>
                </div>
              </div>
            </div>
          </div>
        </div>

        {{-- Transaksi QR --}}
        <div class="card qr">
          <h2>Transaksi QR</h2>
          <p class="muted">Format kode: <code>WH|&lt;SKU&gt;|&lt;SERIAL&gt;</code>. Mode lift harus Otomatis.</p>
          <div class="qr-form">
            <input id="qrCode" placeholder="WH|SKU-001|0001" autocomplete="off" spellcheck="false">
            <div class="seg" role="group" aria-label="Aksi">
              <button id="aStore" aria-pressed="true">Simpan</button>
              <button id="aRetr" aria-pressed="false">Ambil</button>
            </div>
            <select id="qrLevel" aria-label="Lantai tujuan simpan">
              <option value="2">Ke Lantai 2</option>
              <option value="1">Ke Lantai 1</option>
            </select>
            <button class="solid" id="bQr">Kirim perintah</button>
          </div>
          <div class="track" id="track" hidden>
            <div class="track-top"><b id="trkId">–</b><span class="tag" id="trkStatus">–</span></div>
            <ol class="steps" id="trkSteps">
              <li data-s="PENDING">Menunggu</li><li data-s="MOVING">Bergerak</li><li data-s="VERIFYING">Verifikasi</li><li data-s="COMPLETE">Selesai</li>
            </ol>
            <p class="muted" id="trkNote"></p>
          </div>
        </div>

        <div class="card log">
          <h2>Aktivitas sesi ini</h2>
          <ul id="log"><li class="empty"><span></span><span>Belum ada perintah dikirim.</span></li></ul>
        </div>
      </div>
    </section>

    {{-- ---------- BARANG & QR ---------- --}}
    <section class="view" id="view-items">
      <header class="head">
        <div><p class="kicker">Master data</p><h1>Barang &amp; QR</h1></div>
        <button class="solid" id="bNewItem">+ Barang baru</button>
      </header>

      <div class="card table-card">
        <table class="tbl" id="tblItems">
          <thead><tr><th>SKU</th><th>Nama</th><th>Kategori</th><th class="r">Bobot ±</th><th>Stok</th><th></th></tr></thead>
          <tbody><tr><td colspan="6" class="muted">Memuat…</td></tr></tbody>
        </table>
      </div>

      <div class="card" id="unitsCard" hidden>
        <div class="row-between">
          <h2 id="unitsTitle">Unit &amp; QR</h2>
          <button id="bPrint">Cetak label</button>
        </div>
        <div class="qr-grid" id="qrGrid"></div>
      </div>
    </section>

    {{-- ---------- RIWAYAT ---------- --}}
    <section class="view" id="view-moves">
      <header class="head">
        <div><p class="kicker">Jejak audit</p><h1>Riwayat pergerakan</h1></div>
        <div class="filters">
          <input id="mvSearch" placeholder="Cari QR / serial" spellcheck="false">
          <select id="mvType"><option value="">Semua jenis</option><option>IN</option><option>OUT</option><option>ADJUST</option></select>
        </div>
      </header>
      <div class="card table-card">
        <table class="tbl" id="tblMoves">
          <thead><tr><th>Waktu</th><th>Jenis</th><th>SKU</th><th>Serial</th><th>Lantai</th><th class="r">Berat</th><th>Status</th><th>Catatan</th></tr></thead>
          <tbody></tbody>
        </table>
        <div class="pager"><button id="mvPrev">‹ Sebelumnya</button><span id="mvInfo"></span><button id="mvNext">Berikutnya ›</button></div>
      </div>
    </section>

    {{-- ---------- ALARM ---------- --}}
    <section class="view" id="view-alarms">
      <header class="head">
        <div><p class="kicker">Peringatan</p><h1>Alarm</h1></div>
        <div class="seg" role="group" aria-label="Filter alarm">
          <button id="alUnack" aria-pressed="true">Belum diakui</button>
          <button id="alAll" aria-pressed="false">Semua</button>
        </div>
      </header>
      <div id="alarmList" class="alarm-list"></div>
    </section>

    {{-- ---------- PENGATURAN ---------- --}}
    <section class="view" id="view-settings">
      <header class="head">
        <div><p class="kicker">Konfigurasi</p><h1>Pengaturan sistem</h1></div>
        <button class="solid" id="bSaveSettings">Simpan perubahan</button>
      </header>
      <div class="card"><div id="settingsList" class="settings"></div></div>
    </section>

  </main>
</div>

{{-- Modal barang baru --}}
<dialog id="dlgItem">
  <form method="dialog" id="frmItem">
    <h2>Barang baru</h2>
    <div class="form-grid">
      <label>SKU<input name="sku" required placeholder="SKU-001"></label>
      <label>Nama barang<input name="name" required></label>
      <label>Kategori<input name="category"></label>
      <label>Bobot standar (g)<input name="expected_weight_g" type="number" step="0.1" min="0" value="100"></label>
      <label>Toleransi bobot (%)<input name="weight_tolerance_pct" type="number" step="0.1" min="1" value="10"></label>
      <label>Stok minimum<input name="min_stock" type="number" min="0" value="1"></label>
    </div>
    <div class="dlg-foot"><button type="button" id="dlgCancel">Batal</button><button class="solid" type="submit">Simpan</button></div>
  </form>
</dialog>

<div class="toasts" id="toasts" aria-live="polite"></div>
@endsection

@push('scripts')
<script src="{{ asset('assets/dashboard.js') }}"></script>
@endpush
