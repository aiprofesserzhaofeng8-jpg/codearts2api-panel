const maxBytes = 20 * 1024;

function readDataURL(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new Error('图片读取失败'));
    reader.readAsDataURL(file);
  });
}

// Runs only after an explicit upload. Inference and ordinary navigation do not
// load/decode images; the server independently validates every saved avatar.
export async function prepareAvatar(file: File): Promise<string> {
  if (!['image/png', 'image/jpeg', 'image/gif'].includes(file.type)) throw new Error('请选择 PNG、JPEG 或 GIF 图片');
  if (file.size > 5 * 1024 * 1024) throw new Error('原图请控制在 5MB 以内');
  if (file.type === 'image/gif') {
    if (file.size > maxBytes) throw new Error('GIF 请控制在 20KB 以内');
    return readDataURL(file);
  }
  const bitmap = await createImageBitmap(file);
  try {
    const canvas = document.createElement('canvas');
    for (const edge of [256, 192, 128, 64]) {
      const scale = Math.min(1, edge / Math.max(bitmap.width, bitmap.height));
      canvas.width = Math.max(1, Math.round(bitmap.width * scale));
      canvas.height = Math.max(1, Math.round(bitmap.height * scale));
      const context = canvas.getContext('2d');
      if (!context) throw new Error('浏览器无法处理图片');
      context.fillStyle = '#ffffff';
      context.fillRect(0, 0, canvas.width, canvas.height);
      context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
      for (const quality of [.9, .75, .6, .4]) {
        const encoded = canvas.toDataURL('image/jpeg', quality);
        if (atob(encoded.slice(encoded.indexOf(',')+1)).length <= maxBytes) return encoded;
      }
    }
    throw new Error('图片无法压缩到 20KB 以内');
  } finally { bitmap.close(); }
}
