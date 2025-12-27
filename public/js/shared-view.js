//cache busting
const PAGE_CACHE_TOKEN = (() => {
  try {
    return Math.random().toString(36).slice(2, 8);
  } catch {
    return String(Date.now());
  }
})();

function attachThumbnail404Bypass(imgEl, originalSrc) {
  if (!imgEl || !originalSrc) return;
  imgEl.addEventListener('error', () => {
    if (imgEl.dataset.cbTried === '1') return;
    imgEl.dataset.cbTried = '1';
    const sep = originalSrc.includes('?') ? '&' : '?';
    imgEl.src = `${originalSrc}${sep}cb=${encodeURIComponent(PAGE_CACHE_TOKEN)}&t=${Date.now()}`;
  });
}

function formatTimestamp(ts) {
  if (!ts) return 'Unknown';
  const date = new Date(ts * 1000);
  return document.getElementById('useUTC')?.checked
    ? date.toUTCString()
    : date.toLocaleString();
}

function getThumbnailPath(relPath) {
  const dot = relPath.lastIndexOf('.');
  const webp = dot >= 0 ? relPath.slice(0, dot) + '.webp' : relPath + '.webp';
  return 'thumbnails/' + webp.replace(/\\/g, '/'); 
}

function openLightbox(src) {
  const lightbox = document.getElementById('lightbox');
  const img = document.getElementById('lightbox-img');
  img.src = src;
  lightbox.style.display = 'flex';
}

function closeLightbox() {
  document.getElementById('lightbox').style.display = 'none';
}

function togglePass(id) {
  const section = document.getElementById(id);
  const isVisible = section.style.display !== 'none';
  section.style.display = isVisible ? 'none' : 'flex';
  const arrow = section.previousElementSibling.querySelector('.arrow');
  arrow.textContent = isVisible ? '▶' : '▼';
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

async function copyShareLinkForImage(img) {
  const id = img?.id;
  if (!id) {
    console.warn('No image id; cannot build share URL', img);
    return;
  }
  const shareUrl = `${location.origin}/api/share/images/${id}`;

  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(shareUrl);
      console.log('[share] copied:', shareUrl);
    } else {
      // fallback
      prompt('Copy share link:', shareUrl);
    }
  } catch (e) {
    console.warn('[share] clipboard failed:', e);
    prompt('Copy share link:', shareUrl);
  }
}
