# API Reference

## CORS Support

All endpoints support CORS with the following headers:
- `Access-Control-Allow-Origin: *`
- `Access-Control-Allow-Methods: GET, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type, Authorization, Range`

## Endpoints

### 1. Path-Based Proxy (Recommended)

**Format:** `/{domain:port}/{path/to/file}`

**CORS:** ✅ Enabled

**Custom Headers:** ❌ No (uses basic headers only)

**Examples:**
```
GET /f3.megacdn.co:2228/v3-hls-playback/.../playlist.m3u8
GET /rainflare53.pro/file2/.../segment.ts
GET /lightbeam83.wiki/file2/.../segment.jpg
```

**Features:**
- Automatically detects `.m3u8` files and processes them
- Rewrites all URLs to use the same path-based format
- No custom Referer/Origin headers (suitable for most CDNs)

---

### 2. M3U8 Proxy - `/proxy`

**Method:** GET

**CORS:** ✅ Enabled

**Parameters:**
- `url` (required): The URL of the m3u8 playlist
- `headers` (optional): URL-encoded JSON string of custom headers

**Example:**
```
GET /proxy?url=https://example.com/playlist.m3u8
GET /proxy?url=https://example.com/playlist.m3u8&headers=%7B%22Referer%22%3A%22https%3A%2F%2Fexample.com%2F%22%7D
```

**Features:**
- Adds domain-specific Referer/Origin headers (e.g., videostr.net)
- Rewrites all segment URLs to go through `/ts-proxy`
- Handles encryption key URIs

---

### 3. TS Proxy - `/ts-proxy`

**Method:** GET

**CORS:** ✅ Enabled

**Parameters:**
- `url` (required): The URL of the resource (segment, key, image, etc.)
- `headers` (optional): URL-encoded JSON string of custom headers

**Example:**
```
GET /ts-proxy?url=https://example.com/segment.ts
GET /ts-proxy?url=https://example.com/key.key&headers=%7B%22Authorization%22%3A%22Bearer%20token%22%7D
```

**Features:**
- Streams any file type (TS segments, encryption keys, images, etc.)
- Auto-detects content type based on file extension
- Adds domain-specific headers

---

### 4. MP4 Proxy - `/mp4-proxy`

**Method:** GET, OPTIONS

**CORS:** ✅ Enabled

**Parameters:**
- `url` (required): The URL of the MP4 video
- `headers` (optional): URL-encoded JSON string of custom headers

**Example:**
```
GET /mp4-proxy?url=https://example.com/video.mp4
GET /mp4-proxy?url=https://example.com/video.mp4&headers=%7B%22Referer%22%3A%22https%3A%2F%2Fexample.com%2F%22%7D
```

**Features:**
- Supports HTTP range requests (206 Partial Content)
- Forwards `Range` header from client
- Sets `Accept-Ranges: bytes`
- Suitable for video players with seek functionality

---

### 5. Health Check - `/health`

**Method:** GET

**CORS:** ❌ Not needed

**Example:**
```
GET /health
```

**Response:**
```
OK
```

---

## Custom Headers Format

### JavaScript Example

```javascript
const customHeaders = {
  "Referer": "https://example.com/",
  "Origin": "https://example.com",
  "Authorization": "Bearer your-token",
  "Custom-Header": "custom-value"
};

const encodedHeaders = encodeURIComponent(JSON.stringify(customHeaders));
const url = `/proxy?url=https://cdn.example.com/video.m3u8&headers=${encodedHeaders}`;

fetch(url)
  .then(response => response.text())
  .then(playlist => console.log(playlist));
```

### Python Example

```python
import urllib.parse
import json
import requests

custom_headers = {
    "Referer": "https://example.com/",
    "Authorization": "Bearer your-token"
}

encoded_headers = urllib.parse.quote(json.dumps(custom_headers))
url = f"http://localhost:3000/proxy?url=https://cdn.example.com/video.m3u8&headers={encoded_headers}"

response = requests.get(url)
print(response.text)
```

### cURL Example

```bash
# Without custom headers
curl "http://localhost:3000/proxy?url=https://example.com/video.m3u8"

# With custom headers
curl "http://localhost:3000/proxy?url=https://example.com/video.m3u8&headers=%7B%22Referer%22%3A%22https%3A%2F%2Fexample.com%2F%22%7D"
```

---

## Response Headers

### All Endpoints

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization, Range
```

### M3U8 Responses

```
Content-Type: application/vnd.apple.mpegurl
```

### TS/Segment Responses

```
Content-Type: video/mp2t (for .ts files)
Content-Type: application/octet-stream (for other files)
Content-Length: {size}
```

### MP4 Responses

```
Content-Type: video/mp4
Content-Length: {size}
Content-Range: bytes {start}-{end}/{total} (for range requests)
Accept-Ranges: bytes
Content-Disposition: inline
```

---

## Error Responses

All errors return JSON:

```json
{
  "error": "Error message",
  "details": "Additional error details"
}
```

**Common Status Codes:**
- `400 Bad Request`: Missing or invalid parameters
- `500 Internal Server Error`: Server-side error
- `502 Bad Gateway`: Failed to fetch from upstream server

---

## Usage Tips

1. **For most CDNs**: Use the path-based format (`/{domain}/{path}`)
2. **For videostr.net domains**: Use `/proxy` endpoint (adds custom headers)
3. **For range requests**: Use `/mp4-proxy` endpoint
4. **For custom headers**: Add `&headers={encoded_json}` to any endpoint
5. **For CORS**: All endpoints support cross-origin requests

---

## Complete Example

### Video Player Integration

```html
<!DOCTYPE html>
<html>
<head>
  <script src="https://cdn.jsdelivr.net/npm/hls.js@latest"></script>
</head>
<body>
  <video id="video" controls width="640" height="360"></video>
  
  <script>
    const video = document.getElementById('video');
    const proxyUrl = 'http://localhost:3000';
    
    // Method 1: Path-based (simple)
    const videoUrl1 = `${proxyUrl}/f3.megacdn.co:2228/path/to/playlist.m3u8`;
    
    // Method 2: Query-based (with custom headers)
    const originalUrl = 'https://cdn.example.com/video.m3u8';
    const headers = { "Referer": "https://example.com/" };
    const encodedHeaders = encodeURIComponent(JSON.stringify(headers));
    const videoUrl2 = `${proxyUrl}/proxy?url=${encodeURIComponent(originalUrl)}&headers=${encodedHeaders}`;
    
    if (Hls.isSupported()) {
      const hls = new Hls();
      hls.loadSource(videoUrl1); // or videoUrl2
      hls.attachMedia(video);
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = videoUrl1;
    }
  </script>
</body>
</html>
```
