window.addEventListener('load', async () => {
  await fetchOptions();
  await loadImages();

  try {
    const res = await fetch('api/update', { method: 'POST' });
    const data = await res.json();

    if (data.updated) {
      console.log('New data received, reloading images...');
      await loadImages();
    } else {
      console.log('No update needed.');
    }
  } catch (err) {
    console.warn('Update failed or on cooldown.', err);
  }
});

document.getElementById('sortByPass')?.addEventListener('change', () => {
  updateCountLimit();
  loadImages();
});
document.getElementById('bandFilter')?.addEventListener('change', loadImages);
document.getElementById('correctedOnly')?.addEventListener('change', loadImages);
document.getElementById('showUnfilled')?.addEventListener('change', loadImages);
document.getElementById('mapsOnly')?.addEventListener('change', loadImages);
document.getElementById('sortFilter')?.addEventListener('change', loadImages);
document.getElementById('useUTC')?.addEventListener('change', loadImages);
document.getElementById('collapseAll')?.addEventListener('change', collapseAll);

document.getElementById('showCountSelect')?.addEventListener('keypress', (e) => {
  if (e.key === 'Enter') loadImages();
});

function getCountLimit() {
  const groupByPass = document.getElementById('sortByPass')?.checked;
  const showCountInput = document.getElementById('showCountSelect');
  const baseCount = parseInt(showCountInput?.value, 10) || 50;

  if (groupByPass) {
    return { limit: baseCount, type: 'passes' };
  } else {
    return { limit: baseCount, type: 'images' };
  }
}

function updateCountLimit() {
  const groupByPass = document.getElementById('sortByPass')?.checked;
  const showCountLabel = document.getElementById('showCountLabel');
  const showCountInput = document.getElementById('showCountSelect');

  if (!showCountLabel || !showCountInput) return;

  const current = parseInt(showCountInput.value, 10) || 50;

  if (groupByPass) {
    showCountLabel.textContent = 'Passes';
    showCountInput.value = Math.max(1, Math.floor(current * 0.1));
  } else {
    showCountLabel.textContent = 'Images';
    showCountInput.value = Math.max(1, current * 10);
  }
}

function getPassDir(rawPath) {
  const parts = (rawPath || '').split('/');
  parts.pop();
  return parts.join('/');
}

function formatTimestamp(ts) {
  if (!ts) return 'Unknown';
  const date = new Date(ts * 1000);
  if (document.getElementById('useUTC')?.checked) {
    return isNaN(date.getTime()) ? 'Unknown' : date.toUTCString();
  }
  return isNaN(date.getTime()) ? 'Unknown' : date.toLocaleString();
}

function toggleDropdown(event) {
  event.stopPropagation();
  const button = event.currentTarget;
  const dropdownWrapper = button.closest('.dropdown');
  const dropdown = dropdownWrapper?.querySelector('.dropdown-content');
  if (!dropdownWrapper || !dropdown) return;

  dropdownWrapper.classList.toggle('show');

  if (dropdownWrapper.classList.contains('show')) {
    dropdown.style.left = '';
    dropdown.style.right = '';

    const rect = dropdown.getBoundingClientRect();
    const overflowRight = rect.right > window.innerWidth;

    if (overflowRight) {
      const offset = rect.right - window.innerWidth + 10;
      dropdown.style.left = `-${offset}px`;
    }
  }
}

function togglePass(id) {
  const section = document.getElementById(id);
  if (!section) return;

  const isVisible = section.style.display !== 'none';
  section.style.display = isVisible ? 'none' : 'flex';
  const header = section.previousElementSibling;
  const arrow = header?.querySelector('.arrow');
  if (arrow) arrow.textContent = isVisible ? '▶' : '▼';
}

function collapseAll() {
  const controller = document.getElementById('collapseAll')?.checked;
  const sections = document.getElementsByClassName('pass-section');

  for (const section of sections) {
    const images = section.querySelector('.pass-images');
    const arrow = section.querySelector('.arrow');
    if (!images || !arrow) continue;

    if (controller) {
      images.style.display = 'none';
      section.classList.add('collapsed');
      arrow.textContent = '▶';
    } else {
      images.style.display = 'flex';
      section.classList.remove('collapsed');
      arrow.textContent = '▼';
    }
  }
}

function openLightbox(imageSrc) {
  const lightbox = document.getElementById('lightbox');
  const img = document.getElementById('lightbox-img');
  if (!lightbox || !img) return;
  img.src = imageSrc;
  lightbox.style.display = 'flex';
}

function closeLightbox() {
  const lb = document.getElementById('lightbox');
  if (lb) lb.style.display = 'none';
}

document.addEventListener('click', () => {
  document.querySelectorAll('.dropdown-content').forEach(dd => dd.classList.remove('show'));
});

async function fetchOptions() {
  const satSelect = document.getElementById('satelliteFilter');
  const bandSelect = document.getElementById('bandFilter');
  if (!satSelect || !bandSelect) return;

  const satellites = await fetch('/api/satellites').then(res => res.json());
  satSelect.innerHTML = '<option value="">All Satellites</option>' +
    satellites.map(s => `<option value="${s}">${s}</option>`).join('');

  const bands = await fetch('/api/bands').then(res => res.json());
  bandSelect.innerHTML = '<option value="">All Bands</option>' +
    bands.map(b => `<option value="${b}">${b}</option>`).join('');

  satSelect.addEventListener('change', async () => {
    await updateCompositeOptions(satSelect.value);
    await loadImages();
  });

  await updateCompositeOptions('');
}

async function updateCompositeOptions(satellite) {
  const compFilter = document.getElementById('compositeFilter');
  if (!compFilter) return;

  const query = satellite ? `?satellite=${encodeURIComponent(satellite)}` : '';
  const composites = await fetch(`/api/composites${query}`).then(res => res.json());

  compFilter.innerHTML = `
    <div class="composite-actions">
      <button type="button" onclick="selectAllComposites(true)">All</button>
      <button type="button" onclick="selectAllComposites(false)">None</button>
      <button type="button" onclick="loadImages()">Apply</button>
    </div>
    ${composites.map(c => `
      <label>
        <input type="checkbox" value="${c}" class="composite-checkbox" checked>
        ${c}
      </label>
    `).join('')}
  `;
}

function groupImagesBySensor(images) {
  const map = new Map();
  for (const img of images || []) {
    const sensor = img?.sensor || 'Unknown Sensor';
    if (!map.has(sensor)) map.set(sensor, []);
    map.get(sensor).push(img);
  }
  return map;
}

function sortImagesInPlace(images, sorting) {
  if (sorting === 'newest') {
    images.sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0));
  } else if (sorting === 'oldest') {
    images.sort((a, b) => (a.timestamp || 0) - (b.timestamp || 0));
  } else if (sorting === 'hpix') {
    images.sort((a, b) => (b.vPixels || 0) - (a.vPixels || 0));
  } else if (sorting === 'lpix') {
    images.sort((a, b) => (a.vPixels || 0) - (b.vPixels || 0));
  }
}

function selectAllComposites(selectAll) {
  document.querySelectorAll('#compositeFilter .composite-checkbox').forEach(cb => {
    cb.checked = !!selectAll;
  });
}

function getFilters() {
  const satellite = document.getElementById('satelliteFilter')?.value;
  const band = document.getElementById('bandFilter')?.value;
  const selectedComposites = Array.from(document.querySelectorAll('.composite-checkbox:checked')).map(cb => cb.value);
  const sort = document.getElementById('sortFilter')?.value;
  const { limit, type: limitType } = getCountLimit();

  const startDate = document.getElementById('startDate')?.value;
  const endDate = document.getElementById('endDate')?.value;
  const startTime = document.getElementById('startTime')?.value;
  const endTime = document.getElementById('endTime')?.value;

  let sortBy = 'timestamp';
  let sortOrder = 'DESC';
  if (sort === 'oldest') sortOrder = 'ASC';
  if (sort === 'hpix') sortBy = 'vPixels';
  if (sort === 'lpix') {
    sortBy = 'vPixels';
    sortOrder = 'ASC';
  }

  const params = new URLSearchParams();
  if (satellite) params.append('satellite', satellite);
  if (band) params.append('band', band);
  selectedComposites.forEach(c => params.append('composite', c));
  if (limit) params.append('limit', limit);
  if (limitType) params.append('limitType', limitType);
  if (startDate) params.append('startDate', startDate);
  if (endDate) params.append('endDate', endDate);
  if (startTime) params.append('startTime', startTime);
  if (endTime) params.append('endTime', endTime);
  params.append('sortBy', sortBy);
  params.append('sortOrder', sortOrder);

  const mapsOnly = document.getElementById('mapsOnly')?.checked;
  if (mapsOnly) params.append('mapsOnly', '1');

  const correctedOnly = document.getElementById('correctedOnly')?.checked;
  if (correctedOnly) params.append('correctedOnly', '1');

  const showUnfilled = document.getElementById('showUnfilled')?.checked;
  if (!showUnfilled) params.append('filledOnly', '1');

  const utcTime = document.getElementById('useUTC')?.checked;
  if (utcTime) params.append('useUTC', '0');

  return params;
}

async function loadImages() {
  console.log('loadImages called');
  const groupByPass = document.getElementById('sortByPass')?.checked;
  const params = getFilters();

  let images = [];
  try {
    const imagesRes = await fetch(`/api/images?${params.toString()}`);
    if (!imagesRes.ok) {
      console.error('[images] HTTP', imagesRes.status);
    } else {
      const ct = imagesRes.headers.get('content-type') || '';
      if (!ct.includes('application/json')) {
        console.error('[images] Non-JSON response, treating as empty');
      } else {
        const parsed = await imagesRes.json();
        if (Array.isArray(parsed)) {
          images = parsed;
        } else if (parsed && Array.isArray(parsed.images)) {
          images = parsed.images;
        } else if (parsed && Array.isArray(parsed.data)) {
          images = parsed.data;
        } else {
          console.warn('[images] JSON not an array; got:', parsed);
          images = [];
        }
      }
    }
  } catch (err) {
    console.error('Error fetching image data:', err);
  }

  const toSec = (v) => {
    if (typeof v !== 'number') return NaN;
    return v > 1_000_000_000_000 ? Math.floor(v / 1000) : v;
  };
  images.forEach(i => { if (i && i.timestamp != null) i.timestamp = toSec(i.timestamp); });

  const gallery = document.getElementById('gallery');
  if (!gallery) return;
  gallery.innerHTML = '';
  const fragment = document.createDocumentFragment();

  if (groupByPass) {
    gallery.classList.remove('flat-gallery');

    const passGroups = Object.create(null);

    images.forEach(img => {
      const key = img.passId != null ? String(img.passId) : `u-${Math.random()}`;
      let g = passGroups[key];
      if (!g) {
        g = passGroups[key] = {
          type: 'pass',
          satellite: img.satellite,
          timestamp: img.timestamp,
          rawDataPath: img.rawDataPath || 0,
          name: img.name,
          images: [],
          passId: key,
        };
      }
      g.images.push(img);
      if (typeof img.timestamp === 'number' && (typeof g.timestamp !== 'number' || img.timestamp > g.timestamp)) {
        g.timestamp = img.timestamp;
      }
    });

    const renderQueue = Object.values(passGroups)
      .filter(group => Array.isArray(group.images) && group.images.length > 0);

    const sorting = document.getElementById('sortFilter')?.value;
    if (sorting === 'newest') {
      renderQueue.sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0));
    } else if (sorting === 'oldest') {
      renderQueue.sort((a, b) => (a.timestamp || 0) - (b.timestamp || 0));
    }

    renderQueue.forEach((item, index) => {
      const passId = `pass-${index}`;
      const passName = item.name || '';
      const wrapper = document.createElement('div');
      wrapper.className = 'pass-section';
      const parts = String(item.rawDataPath || '').split('.');
      const dataExt = parts.length > 1 ? parts.pop() : '';

      const exportLink = (item.rawDataPath && item.rawDataPath !== 'NOT_CONFIGURED')
        ? `<a href="/api/export?path=${encodeURIComponent(passName + "/" + item.rawDataPath)}" download class="export-raw" title="Download raw data"><b>.${dataExt}</b></a>`
        : '';

      const zipLink = passName
        ? `<a href="/api/zip?path=${encodeURIComponent(passName)}" class="export-zip" title="Download full pass as .zip"><b>.zip</b></a>`
        : '';

      wrapper.innerHTML = `
        <div class="pass-header">
          <div class="pass-title"><strong>${item.satellite || 'Unknown'} - ${formatTimestamp(item.timestamp)}</strong></div>
          <div class="pass-actions">
            ${zipLink}
            ${exportLink}
            <span class="arrow" onclick="togglePass('${passId}')">▼</span>
          </div>
        </div>
        <div class="pass-images" id="${passId}"></div>
      `;

      const passImagesContainer = wrapper.querySelector(`#${passId}`);
if (Array.isArray(item.images) && passImagesContainer) {
  const sorting = document.getElementById('sortFilter')?.value;
  const bySensor = groupImagesBySensor(item.images);
  const sensorNames = Array.from(bySensor.keys()).sort((a, b) => a.localeCompare(b));

  sensorNames.forEach(sensorName => {
    const imagesForSensor = bySensor.get(sensorName) || [];
    sortImagesInPlace(imagesForSensor, sorting);

    const block = document.createElement('div');
    block.className = 'sensor-block';

    const title = document.createElement('div');
    title.className = 'sensor-title';
    title.textContent = sensorName;

    const container = document.createElement('div');
    container.className = 'sensor-images';

    imagesForSensor.forEach(img => container.appendChild(createImageCard(img)));

    block.appendChild(title);
    block.appendChild(container);
    passImagesContainer.appendChild(block);
  });
}
fragment.appendChild(wrapper);
    });

  } else {
    gallery.classList.add('flat-gallery');

    const sorting = document.getElementById('sortFilter')?.value;
    if (sorting === 'newest') {
      images.sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0));
    } else if (sorting === 'oldest') {
      images.sort((a, b) => (a.timestamp || 0) - (b.timestamp || 0));
    } else if (sorting === 'hpix') {
      images.sort((a, b) => (b.vPixels || 0) - (a.vPixels || 0));
    } else if (sorting === 'lpix') {
      images.sort((a, b) => (a.vPixels || 0) - (b.vPixels || 0));
    }

    images.forEach(img => fragment.appendChild(createImageCard(img)));
  }

  gallery.appendChild(fragment);
}

function getThumbnailPath(imagePath) {
  if (!imagePath || typeof imagePath !== 'string') {
    console.warn('Invalid imagePath:', imagePath);
    return '';
  }

  const lastSlashIndex = imagePath.lastIndexOf('/');
  if (lastSlashIndex === -1) return '';

  const dir = imagePath.slice(0, lastSlashIndex);
  const filename = imagePath.slice(lastSlashIndex + 1);
  const filenameWebp = filename.replace(/\.[^/.]+$/, '.webp');

  return `${dir}/thumbnails/${filenameWebp}`;
}

function createImageCard(img) {
  const wrapper = document.createElement('div');
  wrapper.className = 'image-card';
  const imagePath = 'images/' + String(img.path || '').replace(/\\/g, '/');
  const tPath = getThumbnailPath(img.path);

  const dateStr = img.timestamp
    ? new Date(img.timestamp * 1000).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
    : 'Unknown';

  wrapper.innerHTML = `
  <div class="img-wrap" style="position:relative;">
    <a href="${imagePath}" target="_blank">
      <img loading="lazy" src="${tPath}" alt="Image">
    </a>
    <button
      type="button"
      class="share-btn"
      title="Copy share link"
      style="
        position:absolute; top:8px; right:8px;
        z-index:2;
        border:0;
        border-radius:999px;
        padding:6px 10px;
        cursor:pointer;
        background:rgba(0,0,0,.55);
        color:#fff;
        backdrop-filter: blur(4px);
      "
    >🔗</button>
  </div>
  <div class="meta" onclick="openLightbox('${imagePath}')">
    <div><strong>Date:</strong> ${dateStr}</div>
    <div><strong>Satellite:</strong> ${img.satellite ?? ''}</div>
    <div><strong>Composite:</strong> ${img.composite ?? ''}</div>
    <div><strong>Height:</strong> ${img.vPixels ?? ''}px</div>
  </div>
`;
const btn = wrapper.querySelector('.share-btn');
btn?.addEventListener('click', (e) => {
  e.preventDefault();
  e.stopPropagation();
  copyShareLinkForImage(img);
});
  wrapper.classList.add('collapsed');
  return wrapper;
}
