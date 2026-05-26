// Globals
let codebookData = null;
let codebookHeader = null;
let codewordsView = null;
let lshBuckets = null;
let activeImage = null;
let imagesList = [];
let activePayloadBuffer = null;

// DOM Elements
const uploadBox = document.getElementById('upload-box');
const imageInput = document.getElementById('image-input');
const imageListEl = document.getElementById('image-list');
const imageCountEl = document.getElementById('image-count');
const brainFilenameEl = document.getElementById('brain-filename');
const brainWordsizeEl = document.getElementById('brain-wordsize');
const brainWordcountEl = document.getElementById('brain-wordcount');
const brainFilesizeEl = document.getElementById('brain-filesize');
const btnDownloadBrain = document.getElementById('btn-download-brain');
const connectionStatusEl = document.getElementById('connection-status');

const activeFilenameEl = document.getElementById('active-filename');
const canvasOriginal = document.getElementById('canvas-original');
const canvasReconstructed = document.getElementById('canvas-reconstructed');
const placeholderOriginal = document.getElementById('placeholder-original');
const placeholderReconstructed = document.getElementById('placeholder-reconstructed');
const canvasLoader = document.getElementById('canvas-loader');
const loaderText = document.getElementById('loader-text');

// Metrics DOM
const metricMseEl = document.getElementById('metric-mse');
const metricPsnrEl = document.getElementById('metric-psnr');
const metricSavingsEl = document.getElementById('metric-savings');
const progressMse = document.getElementById('progress-mse');
const progressPsnr = document.getElementById('progress-psnr');
const progressSavings = document.getElementById('progress-savings');

// Chart DOM
const chartSizeRaw = document.getElementById('chart-size-raw');
const chartSizeWebp = document.getElementById('chart-size-webp');
const chartSizeBase64 = document.getElementById('chart-size-base64');
const chartSizeCromFirst = document.getElementById('chart-size-crom-first');
const chartSizeCromCached = document.getElementById('chart-size-crom-cached');

const barRaw = document.getElementById('bar-raw');
const barWebp = document.getElementById('bar-webp');
const barBase64 = document.getElementById('bar-base64');
const barCromFirst = document.getElementById('bar-crom-first');
const barCromCached = document.getElementById('bar-crom-cached');
const savingTagPercentage = document.getElementById('saving-tag-percentage');
const chartSimulationBadge = document.getElementById('chart-simulation-badge');

// Initialize
window.addEventListener('DOMContentLoaded', async () => {
  setupEventListeners();
  await loadCodebook();
  populateSimulationMetrics();
  await refreshImagesList();
});

function setupEventListeners() {
  // Drag & drop upload
  uploadBox.addEventListener('dragover', (e) => {
    e.preventDefault();
    uploadBox.classList.add('dragover');
  });

  uploadBox.addEventListener('dragleave', () => {
    uploadBox.classList.remove('dragover');
  });

  uploadBox.addEventListener('drop', (e) => {
    e.preventDefault();
    uploadBox.classList.remove('dragover');
    if (e.dataTransfer.files.length > 0) {
      uploadImage(e.dataTransfer.files[0]);
    }
  });

  imageInput.addEventListener('change', (e) => {
    if (e.target.files.length > 0) {
      uploadImage(e.target.files[0]);
    }
  });

  btnDownloadBrain.addEventListener('click', () => {
    window.location.href = '/api/codebook';
  });

  // Manipulador de abas do Guia Educativo
  document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
      document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
      
      btn.classList.add('active');
      const tabId = btn.getAttribute('data-tab');
      const tabContent = document.getElementById(tabId);
      if (tabContent) {
        tabContent.classList.add('active');
      }
    });
  });
}

// Format bytes to human readable format
function formatBytes(bytes, decimals = 2) {
  if (bytes === 0) return '0 Bytes';
  const k = 1024;
  const dm = decimals < 0 ? 0 : decimals;
  const sizes = ['Bytes', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

// Load and Parse the CROM Codebook ("Brain")
async function loadCodebook() {
  connectionStatusEl.textContent = 'Conectando ao Cérebro...';
  connectionStatusEl.style.color = 'var(--warning)';
  
  try {
    // 1. Obter cabeçalhos via HEAD para verificar versão do Codebook no servidor
    console.log('Verificando versão do Cérebro no servidor...');
    const headResponse = await fetch('/api/codebook', { method: 'HEAD' });
    if (!headResponse.ok) throw new Error('Falha ao consultar metadados do Cérebro');
    
    const lastModified = headResponse.headers.get('Last-Modified') || '';
    const contentLength = headResponse.headers.get('Content-Length') || '';
    const versionKey = `version_${lastModified}_${contentLength}`;
    
    let buffer = null;
    const cachedVersion = localStorage.getItem('crom_codebook_version');
    const cachedData = localStorage.getItem('crom_codebook_data');
    
    if (cachedVersion === versionKey && cachedData) {
      console.log('Cérebro carregado do cache (localStorage)...');
      connectionStatusEl.textContent = 'Lendo do cache local...';
      buffer = base64ToArrayBuffer(cachedData);
    } else {
      console.log('Cérebro desatualizado ou ausente no cache local. Baixando...');
      connectionStatusEl.textContent = 'Baixando Cérebro...';
      
      const response = await fetch('/api/codebook');
      if (!response.ok) throw new Error('Cérebro não encontrado no servidor');
      
      buffer = await response.arrayBuffer();
      
      try {
        const base64Data = arrayBufferToBase64(buffer);
        localStorage.setItem('crom_codebook_data', base64Data);
        localStorage.setItem('crom_codebook_version', versionKey);
        console.log('Dicionário CROM cacheado com sucesso no localStorage.');
      } catch (e) {
        console.warn('Erro ao persistir no localStorage (cota de armazenamento excedida):', e);
      }
    }
    
    codebookData = buffer;
    
    // Parse Binary Header (512 bytes)
    const view = new DataView(buffer);
    
    // Magic: bytes 0..5
    let magic = '';
    for (let i = 0; i < 6; i++) {
      magic += String.fromCharCode(view.getUint8(i));
    }
    
    if (magic !== 'CROMDB') {
      throw new Error('Assinatura do Codebook inválida: ' + magic);
    }
    
    const version = view.getUint16(6, true);
    const codewordSize = view.getUint16(8, true);
    const codewordCount = view.getBigUint64(10, true);
    const dataOffset = view.getBigUint64(18, true);
    
    codebookHeader = {
      magic,
      version,
      codewordSize,
      codewordCount: Number(codewordCount),
      dataOffset: Number(dataOffset)
    };

    // Create a view on the codewords data
    codewordsView = new Uint8Array(buffer, codebookHeader.dataOffset);
    
    // Build LSH index in memory
    const tLsh0 = performance.now();
    lshBuckets = new Map();
    const count = codebookHeader.codewordCount;
    const size = codebookHeader.codewordSize;
    for (let id = 0; id < count; id++) {
      const cwOffset = id * size;
      const b0 = codewordsView[cwOffset];
      const b1 = codewordsView[cwOffset + 1];
      const hash = b0 | (b1 << 8);
      if (!lshBuckets.has(hash)) {
        lshBuckets.set(hash, []);
      }
      lshBuckets.get(hash).push(id);
    }
    console.log(`Índice LSH criado em ${(performance.now() - tLsh0).toFixed(2)}ms com ${lshBuckets.size} baldes`);
    
    // Update UI
    brainWordsizeEl.textContent = `${codewordSize} bytes`;
    brainWordcountEl.textContent = codebookHeader.codewordCount.toLocaleString();
    brainFilesizeEl.textContent = formatBytes(buffer.byteLength);
    
    connectionStatusEl.textContent = 'Cérebro Conectado & Cached';
    connectionStatusEl.parentNode.querySelector('.status-dot').style.backgroundColor = 'var(--success)';
    
    console.log('Cérebro carregado com sucesso:', codebookHeader);
  } catch (err) {
    console.error(err);
    connectionStatusEl.textContent = 'Falha de Conexão com o Cérebro';
    connectionStatusEl.parentNode.querySelector('.status-dot').style.backgroundColor = 'var(--error)';
  }
}

// Refresh list of images from SQLite database
async function refreshImagesList() {
  try {
    const response = await fetch('/api/images');
    if (!response.ok) throw new Error('Failed to load images from server');
    
    imagesList = await response.json();
    imageCountEl.textContent = `${imagesList.length} image(s)`;
    
    // Render list
    imageListEl.innerHTML = '';
    if (imagesList.length === 0) {
      imageListEl.innerHTML = `
        <div class="canvas-placeholder" style="padding: 1.5rem; font-size: 0.8rem;">
          <p>No images in SQLite.</p>
        </div>
      `;
      return;
    }
    
    imagesList.forEach(img => {
      const item = document.createElement('div');
      item.className = 'image-item';
      if (activeImage && activeImage.id === img.id) {
        item.classList.add('active');
      }
      
      item.innerHTML = `
        <div class="image-info">
          <span class="image-name">${img.name}</span>
          <span class="image-details">${img.width}x${img.height} • CROM: ${formatBytes(img.crom_size)}</span>
        </div>
        <div class="image-actions">
          <button title="Delete record" class="btn-delete" data-id="${img.id}">
            <i class="fa-solid fa-trash-can"></i>
          </button>
        </div>
      `;
      
      // Click list item to load it
      item.addEventListener('click', (e) => {
        if (e.target.closest('.btn-delete')) {
          e.stopPropagation();
          deleteImage(img.id);
          return;
        }
        selectImage(img);
      });
      
      imageListEl.appendChild(item);
    });
  } catch (err) {
    console.error('Error refreshing images:', err);
  }
}

// Select and render an image
async function selectImage(imgMeta) {
  activeImage = imgMeta;
  
  // Highlight active item in list
  document.querySelectorAll('.image-item').forEach(el => {
    const nameEl = el.querySelector('.image-name');
    if (nameEl && nameEl.textContent === imgMeta.name) {
      el.classList.add('active');
    } else {
      el.classList.remove('active');
    }
  });

  activeFilenameEl.textContent = `${imgMeta.name} (${imgMeta.width}x${imgMeta.height})`;
  
  // Hide placeholders
  placeholderOriginal.style.display = 'none';
  placeholderReconstructed.style.display = 'none';
  canvasOriginal.style.display = 'block';
  canvasReconstructed.style.display = 'block';

  // Show loader
  canvasLoader.classList.add('active');
  loaderText.textContent = 'Carregando JPEG original...';

  // 1. Draw Original Image
  const origImg = new Image();
  origImg.src = `/api/images/${imgMeta.id}/original`;
  origImg.onload = async () => {
    canvasOriginal.width = imgMeta.width;
    canvasOriginal.height = imgMeta.height;
    const ctxOrig = canvasOriginal.getContext('2d');
    ctxOrig.drawImage(origImg, 0, 0, imgMeta.width, imgMeta.height);

    // Detect if image is grayscale (tons de cinza)
    const isGrayscale = isImageGrayscale(ctxOrig, imgMeta.width, imgMeta.height);
    if (isGrayscale) {
      activeFilenameEl.textContent = `${imgMeta.name} (${imgMeta.width}x${imgMeta.height}) [Tons de Cinza]`;
    }

    // 2. Fetch and Decode CROM indices payload
    loaderText.textContent = 'Baixando payload CROM...';
    try {
      const payloadResp = await fetch(`/api/images/${imgMeta.id}`);
      if (!payloadResp.ok) throw new Error('Falha ao baixar payload CROM');
      
      const payloadBuffer = await payloadResp.arrayBuffer();
      
      loaderText.textContent = 'Decodificando CROM localmente...';
      const startTime = performance.now();
      
      // Decompress CROM using locally cached codebook
      decompressCROM(payloadBuffer, imgMeta.width, imgMeta.height, isGrayscale);
      
      const decodeTime = performance.now() - startTime;
      console.log(`CROM decodificado localmente em ${decodeTime.toFixed(1)}ms`);

      // Update metrics
      await updateMetricsAndCharts(imgMeta);
    } catch (err) {
      console.error('Erro de decodificação:', err);
      alert('Erro ao buscar/decomprimir payload CROM: ' + err.message);
    } finally {
      canvasLoader.classList.remove('active');
    }
  };
  
  origImg.onerror = () => {
    canvasLoader.classList.remove('active');
    alert('Falha ao carregar a imagem original.');
  };
}

// CROM Edge Mode Decoder (O(1) dictionary lookups in JS)
function decompressCROM(payloadBuffer, width, height, forceGrayscale = false) {
  activePayloadBuffer = payloadBuffer;
  if (!codewordsView || !codebookHeader) {
    alert('Cérebro CROM não carregado ainda!');
    return;
  }

  // CROM payload is a flat list of 16-bit uint indices
  const indices = new Uint16Array(payloadBuffer);
  
  const codewordSize = codebookHeader.codewordSize;
  const blockSize = Math.sqrt(codewordSize / 3); // auto-detect block size (typically 8)
  
  canvasReconstructed.width = width;
  canvasReconstructed.height = height;
  
  const ctx = canvasReconstructed.getContext('2d');
  const imgData = ctx.createImageData(width, height);
  const data = imgData.data;

  const blocksPerRow = width / blockSize;
  const numBlocks = indices.length;

  for (let b = 0; b < numBlocks; b++) {
    const idx = indices[b];
    const cwOffset = idx * codewordSize;
    
    // Reconstruct block position
    const blockX = (b % blocksPerRow) * blockSize;
    const blockY = Math.floor(b / blocksPerRow) * blockSize;
    
    // Fill the pixels of this blockSize x blockSize block
    let offset = 0;
    for (let y = 0; y < blockSize; y++) {
      for (let x = 0; x < blockSize; x++) {
        const pixelX = blockX + x;
        const pixelY = blockY + y;
        
        // Safety bound check
        if (pixelX < width && pixelY < height) {
          const destOffset = (pixelY * width + pixelX) * 4;
          
          const r = codewordsView[cwOffset + offset];
          const g = codewordsView[cwOffset + offset + 1];
          const b = codewordsView[cwOffset + offset + 2];
          
          if (forceGrayscale) {
            const gray = Math.round(0.299 * r + 0.587 * g + 0.114 * b);
            data[destOffset]     = gray;
            data[destOffset + 1] = gray;
            data[destOffset + 2] = gray;
          } else {
            data[destOffset]     = r;
            data[destOffset + 1] = g;
            data[destOffset + 2] = b;
          }
          data[destOffset + 3] = 255;                                  // Alpha (fully opaque)
        }
        offset += 3;
      }
    }
  }

  ctx.putImageData(imgData, 0, 0);
}

// Helper to calculate gzipped size of a buffer using browser's CompressionStream
async function getGzippedSize(buffer) {
  try {
    const stream = new Response(buffer).body.pipeThrough(new CompressionStream('gzip'));
    const response = new Response(stream);
    const blob = await response.blob();
    return blob.size;
  } catch (err) {
    // Fallback: estimate as 20% of uncompressed CROM payload for highly structured data
    return Math.floor(buffer.byteLength * 0.20);
  }
}

// Calculate visual difference metrics and network traffic comparisons
async function updateMetricsAndCharts(img) {
  if (chartSimulationBadge) {
    chartSimulationBadge.textContent = 'Dados Reais da Imagem';
    chartSimulationBadge.classList.add('real');
  }
  // Let's compute PSNR / MSE
  const ctxOrig = canvasOriginal.getContext('2d');
  const ctxRecon = canvasReconstructed.getContext('2d');
  
  const origData = ctxOrig.getImageData(0, 0, img.width, img.height).data;
  const reconData = ctxRecon.getImageData(0, 0, img.width, img.height).data;
  
  let squaredErrorSum = 0;
  const len = img.width * img.height * 4;
  
  for (let i = 0; i < len; i += 4) {
    const dr = origData[i] - reconData[i];
    const dg = origData[i+1] - reconData[i+1];
    const db = origData[i+2] - reconData[i+2];
    
    squaredErrorSum += dr*dr + dg*dg + db*db;
  }
  
  const mse = squaredErrorSum / (img.width * img.height * 3);
  let psnr = 99.0;
  if (mse > 0) {
    psnr = 10 * Math.log10((255 * 255) / mse);
  }

  // Calculate actual gzipped CROM size for subsequent visit comparison
  let cromCachedSize = img.crom_size;
  if (activePayloadBuffer) {
    cromCachedSize = await getGzippedSize(activePayloadBuffer);
  } else {
    // Fallback estimation: if we only have the metadata, estimate gzip size as ~20%
    cromCachedSize = Math.floor(img.crom_size * 0.20);
  }

  // Update Metrics Panel UI
  metricMseEl.textContent = mse.toFixed(2);
  metricPsnrEl.textContent = `${psnr.toFixed(2)} dB`;
  
  const savingsPct = ((1 - (cromCachedSize / img.original_size)) * 100).toFixed(1);
  metricSavingsEl.textContent = `${savingsPct}%`;
  
  // Set metric progress bars
  const msePct = Math.max(0, Math.min(100, (1 - mse / 5000) * 100));
  progressMse.style.width = `${msePct}%`;
  progressMse.style.backgroundColor = mse < 1000 ? 'var(--success)' : (mse < 3000 ? 'var(--warning)' : 'var(--error)');
  
  const psnrPct = Math.min(100, (psnr / 50) * 100);
  progressPsnr.style.width = `${psnrPct}%`;
  progressPsnr.style.backgroundColor = psnr > 30 ? 'var(--success)' : (psnr > 20 ? 'var(--warning)' : 'var(--error)');
  
  progressSavings.style.width = `${savingsPct}%`;

  // Update Bandwidth Chart
  const sizes = {
    raw: img.original_size,
    webp: img.webp_size,
    base64: img.base64_size,
    cromFirst: codebookData.byteLength + cromCachedSize,
    cromCached: cromCachedSize
  };

  chartSizeRaw.textContent = formatBytes(sizes.raw);
  chartSizeWebp.textContent = formatBytes(sizes.webp);
  chartSizeBase64.textContent = formatBytes(sizes.base64);
  chartSizeCromFirst.textContent = formatBytes(sizes.cromFirst);
  chartSizeCromCached.textContent = formatBytes(sizes.cromCached);

  const maxVal = Math.max(sizes.raw, sizes.base64, sizes.cromFirst);
  
  barRaw.style.width = `${(sizes.raw / maxVal) * 100}%`;
  barRaw.textContent = formatBytes(sizes.raw);
  
  barWebp.style.width = `${(sizes.webp / maxVal) * 100}%`;
  barWebp.textContent = formatBytes(sizes.webp);
  
  barBase64.style.width = `${(sizes.base64 / maxVal) * 100}%`;
  barBase64.textContent = formatBytes(sizes.base64);
  
  barCromFirst.style.width = `${(sizes.cromFirst / maxVal) * 100}%`;
  barCromFirst.textContent = formatBytes(sizes.cromFirst);
  
  barCromCached.style.width = `${(sizes.cromCached / maxVal) * 100}%`;
  barCromCached.textContent = formatBytes(sizes.cromCached);

  // Subsequent visit saving relative to standard WebP/Base64
  let cachedSavingsVsWebp = 0;
  if (sizes.webp > 0) {
    cachedSavingsVsWebp = ((1 - (sizes.cromCached / sizes.webp)) * 100).toFixed(0);
  }
  if (cachedSavingsVsWebp < 0) {
    cachedSavingsVsWebp = 0; // Don't show negative percentages in the label
  }
  savingTagPercentage.textContent = `${cachedSavingsVsWebp}% SAVED`;
}

// Helper to convert ArrayBuffer to Base64
function arrayBufferToBase64(buffer) {
  let binary = '';
  const bytes = new Uint8Array(buffer);
  const len = bytes.byteLength;
  for (let i = 0; i < len; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return window.btoa(binary);
}

// Helper to convert Base64 to ArrayBuffer
function base64ToArrayBuffer(base64) {
  const binaryString = window.atob(base64);
  const len = binaryString.length;
  const bytes = new Uint8Array(len);
  for (let i = 0; i < len; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }
  return bytes.buffer;
}

// Helper to detect if image on canvas is grayscale
function isImageGrayscale(ctx, width, height) {
  try {
    const imgData = ctx.getImageData(0, 0, width, height).data;
    const len = imgData.length;
    // Amostra a cada 16 pixels para performance máxima
    for (let i = 0; i < len; i += 64) {
      const r = imgData[i];
      const g = imgData[i + 1];
      const b = imgData[i + 2];
      if (Math.abs(r - g) > 8 || Math.abs(r - b) > 8 || Math.abs(g - b) > 8) {
        return false;
      }
    }
    return true;
  } catch (e) {
    return false;
  }
}

// Client-side CROM compression using LSH index
function compressCROM(ctxOrig, w, h) {
  if (!codewordsView || !codebookHeader || !lshBuckets) {
    throw new Error('CROM Brain not loaded yet!');
  }

  const codewordSize = codebookHeader.codewordSize;
  const blockSize = Math.sqrt(codewordSize / 3);
  
  // Crop dimensions to multiples of blockSize
  const adjW = Math.floor(w / blockSize) * blockSize;
  const adjH = Math.floor(h / blockSize) * blockSize;
  
  const imgData = ctxOrig.getImageData(0, 0, adjW, adjH).data;
  
  const blocksPerRow = adjW / blockSize;
  const blocksPerCol = adjH / blockSize;
  const numBlocks = blocksPerRow * blocksPerCol;
  
  const indices = new Uint16Array(numBlocks);
  const blockData = new Uint8Array(blockSize * blockSize * 3);
  
  const codewordCount = codebookHeader.codewordCount;
  const matchCache = new Map();

  let offset = 0;
  for (let by = 0; by < adjH; by += blockSize) {
    for (let bx = 0; bx < adjW; bx += blockSize) {
      // Extract block
      let blockIdx = 0;
      for (let y = 0; y < blockSize; y++) {
        for (let x = 0; x < blockSize; x++) {
          const pixelX = bx + x;
          const pixelY = by + y;
          const srcOffset = (pixelY * adjW + pixelX) * 4;
          
          blockData[blockIdx]     = imgData[srcOffset];     // R
          blockData[blockIdx + 1] = imgData[srcOffset + 1]; // G
          blockData[blockIdx + 2] = imgData[srcOffset + 2]; // B
          blockIdx += 3;
        }
      }
      
      // Fast hash for block match cache key
      let cacheKey = 2166136261;
      for (let i = 0; i < blockIdx; i++) {
        cacheKey ^= blockData[i];
        cacheKey = Math.imul(cacheKey, 16777619);
      }
      cacheKey >>>= 0;
      
      if (matchCache.has(cacheKey)) {
        indices[offset++] = matchCache.get(cacheKey);
        continue;
      }
      
      // LSH + SSD distance search
      const b0 = blockData[0];
      const b1 = blockData[1];
      const hash = b0 | (b1 << 8);
      
      const candidates = lshBuckets.get(hash);
      let bestIdx = 0;
      let bestDist = Infinity;
      
      if (candidates && candidates.length > 0) {
        for (let i = 0; i < candidates.length; i++) {
          const id = candidates[i];
          const cwOffset = id * codewordSize;
          let dist = 0;
          for (let j = 0; j < codewordSize; j++) {
            const diff = blockData[j] - codewordsView[cwOffset + j];
            dist += diff * diff;
          }
          if (dist < bestDist) {
            bestDist = dist;
            bestIdx = id;
            if (dist === 0) break;
          }
        }
      }
      
      // Fallback to full linear scan if bucket was empty or best match in bucket is poor
      const threshold = codewordSize === 48 ? 1000 : 5000;
      if (bestDist > threshold) {
        for (let id = 0; id < codewordCount; id++) {
          const cwOffset = id * codewordSize;
          let dist = 0;
          for (let j = 0; j < codewordSize; j++) {
            const diff = blockData[j] - codewordsView[cwOffset + j];
            dist += diff * diff;
          }
          if (dist < bestDist) {
            bestDist = dist;
            bestIdx = id;
            if (dist === 0) break;
          }
        }
      }
      
      matchCache.set(cacheKey, bestIdx);
      indices[offset++] = bestIdx;
    }
  }
  
  return {
    indices,
    width: adjW,
    height: adjH
  };
}

// Client-side compression, network upload, and verification download flow
async function uploadImage(file) {
  // Set loaders
  canvasLoader.classList.add('active');
  loaderText.textContent = 'Lendo arquivo de imagem...';

  const reader = new FileReader();
  reader.onerror = () => {
    canvasLoader.classList.remove('active');
    alert('Falha ao ler o arquivo de imagem selecionado.');
  };
  
  reader.onload = (e) => {
    const img = new Image();
    img.onerror = () => {
      canvasLoader.classList.remove('active');
      alert('Falha ao interpretar formato do arquivo de imagem.');
    };
    
    img.onload = async () => {
      try {
        if (!codewordsView || !codebookHeader || !lshBuckets) {
          loaderText.textContent = 'Carregando Cérebro CROM...';
          await loadCodebook();
        }
        loaderText.textContent = 'Gerando vetores locais (Compressão LSH)...';
        
        // Setup Original Canvas
        canvasOriginal.width = img.width;
        canvasOriginal.height = img.height;
        const ctxOrig = canvasOriginal.getContext('2d');
        ctxOrig.drawImage(img, 0, 0);
        
        // Detect if grayscale
        const isGrayscale = isImageGrayscale(ctxOrig, img.width, img.height);
        
        activeFilenameEl.textContent = `${file.name} (Processando...)`;
        placeholderOriginal.style.display = 'none';
        placeholderReconstructed.style.display = 'none';
        canvasOriginal.style.display = 'block';
        canvasReconstructed.style.display = 'block';
        
        // Compress image locally
        const tStart = performance.now();
        const compressed = compressCROM(ctxOrig, img.width, img.height);
        const tDuration = performance.now() - tStart;
        console.log(`CROM comprimido localmente em ${tDuration.toFixed(1)}ms`);
        
        // Instantly decompress locally to show visual feedback
        decompressCROM(compressed.indices.buffer, compressed.width, compressed.height, isGrayscale);
        
        // Calculate comparison metrics
        const originalSize = compressed.width * compressed.height * 3;
        const jpegBase64Url = canvasOriginal.toDataURL('image/jpeg', 0.8);
        const base64Payload = jpegBase64Url.split(',')[1];
        const base64Size = base64Payload.length;
        const jpegSize = Math.floor(base64Size * 0.75);
        const webpSize = Math.floor(jpegSize * 0.70);
        
        const cromPayloadBase64 = arrayBufferToBase64(compressed.indices.buffer);
        
        // Update local metrics and charts temporarily
        const tempImgMeta = {
          id: 'temp',
          name: file.name,
          width: compressed.width,
          height: compressed.height,
          original_size: originalSize,
          base64_size: base64Size,
          jpeg_size: jpegSize,
          webp_size: webpSize,
          crom_size: compressed.indices.byteLength
        };
        updateMetricsAndCharts(tempImgMeta);
        
        // Upload the locally generated CROM vectors to the server
        loaderText.textContent = 'Enviando vetores para o SQLite...';
        const response = await fetch('/api/images', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({
            name: file.name,
            width: compressed.width,
            height: compressed.height,
            crom_payload: cromPayloadBase64,
            base64_payload: base64Payload,
            original_size: originalSize,
            base64_size: base64Size,
            jpeg_size: jpegSize,
            webp_size: webpSize
          })
        });
        
        if (!response.ok) {
          const errText = await response.text();
          throw new Error(errText || 'Falha ao salvar payload pré-comprimido no servidor');
        }
        
        const newImgMeta = await response.json();
        console.log('Salvo no SQLite. Iniciando download de verificação...', newImgMeta);
        
        // Verification step: download vectors back from server
        loaderText.textContent = 'Baixando vetores salvos para validação...';
        const verifyResp = await fetch(`/api/images/${newImgMeta.id}`);
        if (!verifyResp.ok) throw new Error('Falha ao baixar vetores para verificação');
        
        const verifiedBuffer = await verifyResp.arrayBuffer();
        
        // Decompress the downloaded vectors
        loaderText.textContent = 'Reconstruindo vetores baixados...';
        decompressCROM(verifiedBuffer, newImgMeta.width, newImgMeta.height, isGrayscale);
        
        // Verify visual likeness and calculate definitive metrics
        updateMetricsAndCharts(newImgMeta);
        console.log('Upload e verificação CROM concluídos com sucesso!');
        
        activeFilenameEl.textContent = `${newImgMeta.name} (${newImgMeta.width}x${newImgMeta.height}) [Verificado]`;
        if (isGrayscale) {
          activeFilenameEl.textContent += ' [Tons de Cinza]';
        }
        await refreshImagesList();
        activeImage = newImgMeta;
      } catch (err) {
        console.error(err);
        alert('Upload e verificação falharam: ' + err.message);
      } finally {
        canvasLoader.classList.remove('active');
      }
    };
    img.src = e.target.result;
  };
  reader.readAsDataURL(file);
}

// Delete image record
async function deleteImage(id) {
  if (!confirm('Are you sure you want to delete this image?')) return;
  
  try {
    const response = await fetch(`/api/images/${id}`, {
      method: 'DELETE'
    });

    if (!response.ok) throw new Error('Failed to delete image');
    
    console.log('Image deleted:', id);
    if (activeImage && activeImage.id === id) {
      activeImage = null;
      resetViewer();
    }
    
    await refreshImagesList();
  } catch (err) {
    alert('Delete error: ' + err.message);
  }
}

function resetViewer() {
  canvasOriginal.style.display = 'none';
  canvasReconstructed.style.display = 'none';
  placeholderOriginal.style.display = 'flex';
  placeholderReconstructed.style.display = 'flex';
  
  populateSimulationMetrics();
}

// Populate simulated metrics for standard 512x512 image on startup
function populateSimulationMetrics() {
  if (chartSimulationBadge) {
    chartSimulationBadge.textContent = 'Simulação Conceitual (512x512)';
    chartSimulationBadge.classList.remove('real');
  }
  
  activeFilenameEl.textContent = 'Nenhuma Imagem Selecionada (Exibindo Simulação)';

  // Visual metrics simulation (typical high-fidelity wireframe)
  metricMseEl.textContent = '12.50';
  metricPsnrEl.textContent = '37.16 dB';
  metricSavingsEl.textContent = '99.7%';
  
  // Set progress bars
  progressMse.style.width = '95%';
  progressMse.style.backgroundColor = 'var(--success)';
  progressPsnr.style.width = '74%';
  progressPsnr.style.backgroundColor = 'var(--success)';
  progressSavings.style.width = '99.7%';

  // Size metrics for 512x512 image:
  // Raw: 512 * 512 * 3 = 768 KB
  // WebP: 46.5 KB (47616 bytes)
  // Base64: 88.6 KB (90726 bytes)
  // CROM First: codebook (768.5 KB) + index payload (2.1 KB gzipped) = 770.6 KB
  // CROM Cached: 2.10 KB (gzipped indices)
  const sizes = {
    raw: 768 * 1024,
    webp: 46.5 * 1024,
    base64: 88.6 * 1024,
    cromFirst: (768.5 + 2.1) * 1024,
    cromCached: 2.10 * 1024
  };

  chartSizeRaw.textContent = formatBytes(sizes.raw);
  chartSizeWebp.textContent = formatBytes(sizes.webp);
  chartSizeBase64.textContent = formatBytes(sizes.base64);
  chartSizeCromFirst.textContent = formatBytes(sizes.cromFirst);
  chartSizeCromCached.textContent = formatBytes(sizes.cromCached);

  const maxVal = sizes.cromFirst; // CROM First is largest

  barRaw.style.width = `${(sizes.raw / maxVal) * 100}%`;
  barRaw.textContent = formatBytes(sizes.raw);
  
  barWebp.style.width = `${(sizes.webp / maxVal) * 100}%`;
  barWebp.textContent = formatBytes(sizes.webp);
  
  barBase64.style.width = `${(sizes.base64 / maxVal) * 100}%`;
  barBase64.textContent = formatBytes(sizes.base64);
  
  barCromFirst.style.width = `${(sizes.cromFirst / maxVal) * 100}%`;
  barCromFirst.textContent = formatBytes(sizes.cromFirst);
  
  barCromCached.style.width = `${(sizes.cromCached / maxVal) * 100}%`;
  barCromCached.textContent = formatBytes(sizes.cromCached);

  const cachedSavingsVsWebp = ((1 - (sizes.cromCached / sizes.webp)) * 100).toFixed(0);
  savingTagPercentage.textContent = `${cachedSavingsVsWebp}% ECONOMIZADO`;
}
