document.addEventListener('DOMContentLoaded', async () => {
const uOpenBtn    = document.getElementById('btn-open-users');
const uMsg        = document.getElementById('users-msg');
const uBtnAdd     = document.getElementById('btn-add-user');
const uBtnSave    = document.getElementById('btn-save-users');
const updateCdInput   = document.getElementById('update-cd');
const passLimitInput  = document.getElementById('pass-limit');

const saveSettingsBtn    = document.getElementById('settings-save');
const statusEl   = document.getElementById('settings-status');

function updateArchiveVisibility() {
  archBlock.style.display = archToggle.checked ? 'block' : 'none';
}
archToggle.addEventListener('change', updateArchiveVisibility);

  // --- Prefill from server ---
async function prefillSettings() {
    statusEl.textContent = 'Loading…';
    //await loadSatdumpList();
    try {
      const res = await fetch('/local/api/settings', { method: 'GET' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const settings = await res.json(); 

      if (typeof settings['hwmonitor'] === 'string') {
        const v = settings['hwmonitor'].toLowerCase();
        if (['off', 'hwinfo', 'native'].includes(v)) {
          hwSelect.value = v;
        }
      }

      const active = (settings['archive.active'] === '1');
      archToggle.checked = active;

      if (active) {
        if (settings['archive.span']) {
          const span = parseInt(settings['archive.span'], 10);
          if (!isNaN(span) && span > 0) archSpan.value = String(span);
        }
        if (typeof settings['archive.retainData'] === 'string') {
          archRetain.checked = (settings['archive.retainData'].toLowerCase() === 'true');
        }
      }
      updateArchiveVisibility();
      if (settings['archive.clean'] != null) {
        const clean = parseInt(settings['archive.clean'], 10);
        if (!isNaN(clean) && clean >= 0) cleanDays.value = String(clean);
      }
      if (settings['update_cd'] != null) {
  const v = parseInt(settings['update_cd'], 10);
  if (!isNaN(v) && v >= 0) updateCdInput.value = String(v);
}
if (settings['pass_limit'] != null) {
  const v = parseInt(settings['pass_limit'], 10);
  if (!isNaN(v) && v >= 0) passLimitInput.value = String(v);
}
if (settings['satdump_rate'] != null) {
  const v = parseInt(settings['satdump_rate'], 10);
  if (!isNaN(v) && v >= 0) satRateInput.value = String(v);
}
if (settings['satdump_span'] != null) {
  const v = parseInt(settings['satdump_span'], 10);
  if (!isNaN(v) && v >= 0) satSpanInput.value = String(v);
}

      statusEl.textContent = 'Loaded';
      setTimeout(()=>{ statusEl.textContent=''; }, 1500);
    } catch (err) {
      console.error(err);
      statusEl.textContent = `Load failed: ${err.message}`;
    }
}

async function saveSettings() {
    const payload = {};

    payload['hwmonitor'] = hwSelect.value;

    const on = archToggle.checked;
    payload['archive.active'] = on ? '1' : '0';

    if (on) {
      const spanVal = Math.max(1, parseInt(archSpan.value || '60', 10));
      payload['archive.span'] = String(spanVal);
    }

    const cleanVal = Math.max(0, parseInt(cleanDays.value || '0', 10));
    payload['archive.clean'] = String(cleanVal);

    {
  const v = parseInt(updateCdInput.value || '0', 10);
  if (!isNaN(v) && v >= 0) payload['update_cd'] = String(v);
}
{
  const v = parseInt(passLimitInput.value || '0', 10);
  if (!isNaN(v) && v >= 0) payload['pass_limit'] = String(v);
}
{
  const v = parseInt(satRateInput.value || '0', 10);
  if (!isNaN(v) && v >= 0) payload['satdump_rate'] = String(v);
}
{
  const v = parseInt(satSpanInput.value || '0', 10);
  if (!isNaN(v) && v >= 0) payload['satdump_span'] = String(v);
}

    statusEl.textContent = 'Saving…';

    try {
      const res = await fetch('/local/api/settings', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(payload)
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || data.message || `HTTP ${res.status}`);
      statusEl.textContent = `Saved (${data.updated} updated)`;
      setTimeout(()=>{ statusEl.textContent=''; }, 2500);
    } catch (err) {
      console.error(err);
      statusEl.textContent = `Save failed: ${err.message}`;
    }
}

  
  function toastOk(t){ cmsg.textContent = t; cmsg.classList.remove('comp-bad'); cmsg.classList.add('comp-ok'); }
  function toastErr(t){ cmsg.textContent = t; cmsg.classList.remove('comp-ok'); cmsg.classList.add('comp-bad'); }
  function utoastOk(t){ uMsg.textContent = t; uMsg.classList.remove('comp-bad'); uMsg.classList.add('comp-ok'); }
  function utoastErr(t){ uMsg.textContent = t; uMsg.classList.remove('comp-ok'); uMsg.classList.add('comp-bad'); }
  
  function showToast(msg) {
  let toast = document.createElement("div");
  toast.textContent = msg;
  toast.style.position = "fixed";
  toast.style.bottom = "20px";
  toast.style.right = "20px";
  toast.style.background = "rgba(0,0,0,0.8)";
  toast.style.color = "#fff";
  toast.style.padding = "10px 15px";
  toast.style.borderRadius = "6px";
  toast.style.zIndex = 9999;
  document.body.appendChild(toast);

  setTimeout(() => {
    toast.style.transition = "opacity 0.5s";
    toast.style.opacity = 0;
    setTimeout(() => toast.remove(), 500);
  }, 4000);
}

umodal.addEventListener('click', (e) => { if (e.target.dataset.uclose) ucloseModal(); });
uOpenBtn?.addEventListener('click', uopenModal);
uBtnAdd?.addEventListener('click', () => uaddRow());
uBtnSave?.addEventListener('click', usaveAll);
saveSettingsBtn.addEventListener('click', saveSettings);
prefillSettings();
btnOpen.addEventListener('click', copenModal);
cmodal.addEventListener('click', (e) => { if (e.target.dataset.close) ccloseModal(); });
btnAdd.addEventListener('click', () => caddRow());
btnSave.addEventListener('click', csaveAll);
document.getElementById("repopulateBtn").addEventListener("click", async () => {
  try {
    const resp = await fetch("/api/repopulate", {
      method: "POST",
      headers: { "Content-Type": "application/json" }
    });

    const data = await resp.json();

    if (resp.ok) {
      const dur = data.duration_ms ? `${data.duration_ms} ms` : "";
      showToast(`${data.message} (${dur})`);
    } else {
      showToast(`Error: ${data.message || "unknown error"}`);
    }
  } catch (err) {
    showToast("Network error: " + err.message);
  }
});

  window.openThemePopup = openThemePopup;

});