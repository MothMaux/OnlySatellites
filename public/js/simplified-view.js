document.addEventListener('DOMContentLoaded', () => {
  renderSimplifiedImages(initialData);
  document.getElementById('collapseAll')?.addEventListener('change', collapseAll);
});

window.addEventListener('load', async () => {
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

function createImageCard(img, pass) {
  const wrapper = document.createElement('div');
  wrapper.className = 'image-card';
  const imagePath = "images/" + img.path.replace(/\\/g, '/');
  const tPath = getThumbnailPath(img.path);

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
    <div><strong>Date:</strong> ${pass.timestamp ? new Date(pass.timestamp * 1000).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : 'Unknown'}</div>
    <div><strong>Satellite:</strong> ${pass.satellite}</div>
    <div><strong>Sensor:</strong> ${img.sensor}</div>
    <div><strong>Composite:</strong> ${img.composite}</div>
    <div><strong>Height:</strong> ${img.vPixels}px</div>
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

function groupImagesBySensor(images) {
  const map = new Map();
  for (const img of images || []) {
    const sensor = (img && img.sensor) ? img.sensor : 'Unknown Sensor';
    if (!map.has(sensor)) map.set(sensor, []);
    map.get(sensor).push(img);
  }
  return map;
}

function renderSimplifiedImages(passes) {
  const gallery = document.getElementById('gallery');
  gallery.innerHTML = '';
  gallery.classList.remove('flat-gallery');

  const fragment = document.createDocumentFragment();

  passes.forEach((pass, index) => {
    const passId = `pass-${index}`;
    const wrapper = document.createElement('div');
    wrapper.className = 'pass-section';
    const parts = pass.rawDataPath.split(".");
    const dataExt = parts.pop(); 

    const exportLink = (pass.rawDataPath && pass.rawDataPath !== 'NOT_CONFIGURED')
      ? `<a href="/api/export?path=${encodeURIComponent(pass.name + "/" + pass.rawDataPath)}" download class="export-raw" title="Download raw data"><b>.${dataExt}</b></a>`
      : '';

    const zipLink = pass.name
      ? `<a href="/api/zip?path=${encodeURIComponent(pass.name)}" class="export-zip" title="Download full pass as .zip"><b>.zip</b></a>`
      : '';

    wrapper.innerHTML = `
      <div class="pass-header">
        <div class="pass-title"><strong>${pass.satellite || 'Unknown'} - ${formatTimestamp(pass.timestamp)}</strong></div>
        <div class="pass-actions">
          ${zipLink}
          ${exportLink}
          <span class="arrow" onclick="togglePass('${passId}')"></span>
        </div>
      </div>
      <div class="pass-images" id="${passId}"></div>
    `;

    const passImagesContainer = wrapper.querySelector(`#${passId}`);
    if (Array.isArray(pass.images) && passImagesContainer) {
  const bySensor = groupImagesBySensor(pass.images);
  const sensorNames = Array.from(bySensor.keys()).sort((a, b) => a.localeCompare(b)); // A→Z

  sensorNames.forEach(sensorName => {
    const block = document.createElement('div');
    block.className = 'sensor-block';

    // Title (NOT collapsible)
    const title = document.createElement('div');
    title.className = 'sensor-title';
    title.textContent = sensorName;

    const container = document.createElement('div');
    container.className = 'sensor-images';

    (bySensor.get(sensorName) || []).forEach(img => {
      container.appendChild(createImageCard(img, pass));
    });

    block.appendChild(title);
    block.appendChild(container);
    passImagesContainer.appendChild(block);
  });
}

    if (index === 0) {
      passImagesContainer.style.display = 'flex';
      wrapper.classList.remove('collapsed');
      wrapper.querySelector('.arrow').textContent = '▼';
    } else {
      passImagesContainer.style.display = 'none';
      wrapper.classList.add('collapsed');
      wrapper.querySelector('.arrow').textContent = '▶';
    }

    fragment.appendChild(wrapper);
  });

  gallery.appendChild(fragment);
}