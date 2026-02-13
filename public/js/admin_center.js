document.addEventListener('DOMContentLoaded', async () => {
const umodal      = document.getElementById('users-modal');
const uOpenBtn    = document.getElementById('btn-open-users');
const uTbody      = document.querySelector('#users-table tbody');
const uMsg        = document.getElementById('users-msg');
const uBtnAdd     = document.getElementById('btn-add-user');
const uBtnSave    = document.getElementById('btn-save-users');
const updateCdInput   = document.getElementById('update-cd');
const passLimitInput  = document.getElementById('pass-limit');

const saveSettingsBtn    = document.getElementById('settings-save');
const statusEl   = document.getElementById('settings-status');


  // --- Archiving dependent block ---
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
  function uclearMsg(){ uMsg.textContent=''; uMsg.classList.remove('comp-ok','comp-bad'); }
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

  

  

  
  async function uopenModal() {
  uclearMsg();
  umodal.classList.remove('hidden');
  await uloadUsers();
}
function ucloseModal() {
  umodal.classList.add('hidden');
  uTbody.innerHTML = '';
}

umodal.addEventListener('click', (e) => { if (e.target.dataset.uclose) ucloseModal(); });
uOpenBtn?.addEventListener('click', uopenModal);

function uaddRow(u = { id:null, username:'', level:5 }, isNew = true) {
  const tr = document.createElement('tr');
  tr.dataset.new = isNew ? '1' : '0';
  tr.dataset.id  = u.id ?? '';

  tr.innerHTML = `
    <td>
      <input type="text" class="u-username" value="${escapeHtml(u.username||'')}" ${isNew ? '' : ''} aria-label="Username">
    </td>
    <td>
      <input type="number" class="u-level" min="0" max="10" step="1" value="${Number.isFinite(u.level)? u.level : 5}" aria-label="Auth level">
    </td>
    <td style="display:flex; gap:6px; align-items:center;">
      <input type="text" class="u-password" placeholder="(leave blank to keep)" aria-label="Password">
      <button type="button" class="u-gen" title="Generate">🎲</button>
    </td>
    <td>
      <button type="button" class="u-reset">Reset</button>
      <button type="button" class="u-del" ${isNew?'disabled':''} title="Delete">Delete</button>
    </td>
  `;

  // Wire buttons
  const genBtn   = tr.querySelector('.u-gen');
  const resetBtn = tr.querySelector('.u-reset');
  const delBtn   = tr.querySelector('.u-del');

  genBtn.addEventListener('click', () => {
    // client-side random; server will hash on reset
    const pw = Math.random().toString(36).slice(-10) + '!';
    tr.querySelector('.u-password').value = pw;
  });

  resetBtn.addEventListener('click', async () => {
    uclearMsg();
    const id = Number(tr.dataset.id||0);
    const pw = tr.querySelector('.u-password').value.trim();
    if (!id) { utoastErr('Save row first, then reset password.'); return; }
    if (!pw) { // ask server to generate if empty
      try {
        const res = await fetch(`/local/api/users/${id}/reset-password`, {
          method:'POST', headers:{'Content-Type':'application/json'}, credentials:'include',
          body: JSON.stringify({ generate: true })
        });
        const data = await res.json().catch(()=> ({}));
        if (!res.ok) throw new Error(data.error || data.message || `HTTP ${res.status}`);
        tr.querySelector('.u-password').value = data.newPassword || '';
        utoastOk('Password generated.');
      } catch (e) { utoastErr('Reset failed: '+e.message); }
      return;
    }
    // explicit new password
    try {
      const res = await fetch(`/local/api/users/${id}/reset-password`, {
        method:'POST', headers:{'Content-Type':'application/json'}, credentials:'include',
        body: JSON.stringify({ newPassword: pw })
      });
      if (!res.ok) throw new Error(await res.text().catch(()=>`HTTP ${res.status}`));
      utoastOk('Password updated.');
    } catch (e) { utoastErr('Reset failed: '+e.message); }
  });

  delBtn.addEventListener('click', async () => {
    uclearMsg();
    if (tr.dataset.new === '1') { tr.remove(); return; }
    const id = Number(tr.dataset.id||0);
    if (!id) return;
    if (!confirm(`Delete user "${tr.querySelector('.u-username').value}"?`)) return;
    try {
      const res = await fetch(`/local/api/users/${id}`, { method:'DELETE', credentials:'include' });
      if (!res.ok) throw new Error(await res.text().catch(()=>`HTTP ${res.status}`));
      tr.remove();
      utoastOk('User deleted.');
    } catch (e) { utoastErr('Delete failed: '+e.message); }
  });

  uTbody.appendChild(tr);
}

async function uloadUsers() {
  uTbody.innerHTML = '';
  try {
    const res = await fetch('/local/api/users', { credentials:'include' });
    if (!res.ok) throw new Error('Failed to fetch users');
    const list = await res.json();
    if (list != null)
    {
      list.forEach(u => uaddRow({ id:u.id, username:u.username, level: u.level }, false));
    }
  } catch (e) {
    utoastErr(e.message);
  }
}

async function usaveAll() {
  uclearMsg();
  uBtnSave.disabled = true;
  try {
    const rows = Array.from(uTbody.querySelectorAll('tr'));
    for (const tr of rows) {
      const isNew   = tr.dataset.new === '1';
      const id      = Number(tr.dataset.id||0);
      const user    = tr.querySelector('.u-username').value.trim();
      const level   = parseInt(tr.querySelector('.u-level').value, 10);
      const pwField = tr.querySelector('.u-password');
      const pw      = (pwField.value||'').trim();

      if (!user || isNaN(level) || level<0 || level>10) {
        throw new Error('Each row needs a username and a level 0..10.');
      }

      if (isNew) {
        if (!pw) throw new Error(`New user "${user}" requires a password (or use 🎲 then Save).`);
        const res = await fetch('/local/api/users', {
          method: 'POST',
          headers: {'Content-Type':'application/json'},
          credentials:'include',
          body: JSON.stringify({ username:user, level, password: pw })
        });
        const data = await res.json().catch(()=> ({}));
        if (!res.ok) throw new Error(data.error || `Create failed for "${user}"`);
        tr.dataset.new = '0';
        tr.dataset.id  = data.id;
        tr.querySelector('.u-del').disabled = false;
        pwField.value = '';
      } else {
        {
          const res = await fetch(`/local/api/users/${id}/username`, {
            method:'PUT', headers:{'Content-Type':'application/json'}, credentials:'include',
            body: JSON.stringify({ username: user })
          });
          if (!res.ok) throw new Error(`Update username failed for id=${id}`);
        }
        {
          const res = await fetch(`/local/api/users/${id}/level`, {
            method:'PUT', headers:{'Content-Type':'application/json'}, credentials:'include',
            body: JSON.stringify({ level })
          });
          if (!res.ok) throw new Error(`Update level failed for id=${id}`);
        }
        if (pw) {
          const res = await fetch(`/local/api/users/${id}/reset-password`, {
            method:'POST', headers:{'Content-Type':'application/json'}, credentials:'include',
            body: JSON.stringify({ newPassword: pw })
          });
          if (!res.ok) throw new Error(`Password reset failed for "${user}"`);
          pwField.value = '';
        }
      }
    }
    utoastOk('All users saved.');
    await uloadUsers();
  } catch (e) {
    utoastErr(e.message);
  } finally {
    uBtnSave.disabled = false;
  }
}



 uBtnAdd?.addEventListener('click', () => uaddRow());
 uBtnSave?.addEventListener('click', usaveAll);

  saveSettingsBtn.addEventListener('click', saveSettings);

  updateArchiveVisibility();
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

  if (!statsDiv) return;

  try {
    const res = await fetch('api/disk-stats');
    const data = await res.json();

    if (data.error) {
      statsDiv.innerHTML = `<p>Error fetching data: ${data.error}</p>`;
      return;
    }

    const formatBytes = (bytes) => {
      const units = ['B', 'KB', 'MB', 'GB', 'TB'];
      let i = 0;
      while (bytes >= 1024 && i < units.length - 1) {
        bytes /= 1024;
        i++;
      }
      return `${bytes.toFixed(1)} ${units[i]}`;
    };

    statsDiv.innerHTML = `
      <h2>Disk & Retention Stats</h2>
      <ul>
        <li><strong>Total Disk Size:</strong> ${formatBytes(data.disk.total)}</li>
        <li><strong>Free Disk Space:</strong> ${formatBytes(data.disk.free)}</li>
        <li><strong>Live Output Total Size:</strong> ${formatBytes(data.live_output.totalSize)}</li>
        <li><strong>Live Output (Past 2 Weeks):</strong> ${formatBytes(data.live_output.recentSize)}</li>
        <li><strong>Approx. Data Retention Span:</strong> ${data.estimates.dataRetentionDays ?? 'Unknown'} days</li>
        <li><strong>Approx. Time Until Disk Full:</strong> ${data.estimates.timeToDiskFullDays ?? 'Unknown'} days</li>
      </ul>
    `;
  } catch (err) {
    console.error('Failed to fetch admin stats:', err);
    statsDiv.innerHTML = `<p>Error loading data.</p>`;
  }
});