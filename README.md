# Go M3U8 Proxy Server

A high-performance proxy server written in Go for handling M3U8 playlists, TS segments, MP4 videos, and general content.

## Features

- **M3U8 Playlist Proxying**: Automatically rewrites M3U8 playlists to proxy all segments and keys
- **TS Segment Proxying**: Handles video segments with proper content types
- **MP4 Video Proxying**: Supports range requests for video streaming
- **Custom Headers**: Support for custom headers via query parameters
- **CORS Support**: Configurable CORS with origin whitelisting
- **Generic Fetch**: Proxy any URL with optional referer

## Installation

1. **Clone or navigate to the project directory**

2. **Install dependencies**:
```bash
go mod download
```

3. **Create environment file**:
```bash
cp .env.example .env
```

4. **Configure your settings** in `.env`:
```env
HOST=localhost
PORT=3000
PUBLIC_URL=http://localhost:3000
ALLOWED_ORIGINS=
```

## Usage

### Start the server

```bash
go run .
```

Or build and run:

```bash
go build -o proxy-server
./proxy-server
```

### Endpoints

#### 1. **M3U8 Proxy** - `/proxy`
Proxies M3U8 playlists and rewrites all URLs to go through the proxy.

```
GET /proxy?url={m3u8_url}&headers={optional_headers_json}
```

**Example**:
```
http://localhost:3000/proxy?url=https://example.com/playlist.m3u8
```

#### 2. **TS/Segment Proxy** - `/ts-proxy`
Proxies video segments, encryption keys, and other files.

```
GET /ts-proxy?url={segment_url}&headers={optional_headers_json}
```

**Example**:
```
http://localhost:3000/ts-proxy?url=https://example.com/segment.ts
```

#### 3. **MP4 Proxy** - `/mp4-proxy`
Proxies MP4 videos with range request support.

```
GET /mp4-proxy?url={mp4_url}&headers={optional_headers_json}
```

**Example**:
```
http://localhost:3000/mp4-proxy?url=https://example.com/video.mp4
```

#### 4. **Generic Fetch** - `/fetch`
Fetches any URL with optional referer.

```
GET /fetch?url={any_url}&ref={optional_referer}
```

**Example**:
```
http://localhost:3000/fetch?url=https://example.com/image.jpg&ref=https://example.com
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `HOST` | Server host | `localhost` |
| `PORT` | Server port | `3000` |
| `PUBLIC_URL` | Public URL for proxy links | `http://HOST:PORT` |
| `ALLOWED_ORIGINS` | Comma-separated allowed origins | Empty (allow all) |

### CORS Configuration

- **Allow all origins**: Leave `ALLOWED_ORIGINS` empty
- **Restrict origins**: Set `ALLOWED_ORIGINS=http://localhost:5173,https://example.com`

## Project Structure

```
go-proxy/
├── main.go          # Server setup and routing
├── handlers.go      # HTTP request handlers
├── utils.go         # Helper functions
├── v3_proxy.go      # V3 HLS playback proxy
├── file_proxy.go    # File proxy handler
├── go.mod           # Go module definition
├── .env.example     # Example environment configuration
└── README.md        # This file
```

## Development

### Custom Headers

You can pass custom headers via the `headers` query parameter as a JSON object:

```bash
# Example with custom headers
curl "http://localhost:3000/proxy?url=https://example.com/playlist.m3u8&headers=%7B%22referer%22%3A%22https%3A%2F%2Fexample.com%22%2C%22origin%22%3A%22https%3A%2F%2Fexample.com%22%7D"
```

### Testing

Test the proxy with curl:

```bash
# Test M3U8 proxy
curl "http://localhost:3000/proxy?url=https://example.com/playlist.m3u8"

# Test with custom headers
curl "http://localhost:3000/ts-proxy?url=https://example.com/segment.ts&headers=%7B%22referer%22%3A%22https%3A%2F%2Fexample.com%22%7D"
```

## License

MIT

## Notes

- The server automatically handles redirects (up to 5)
- Request timeout is set to 30 seconds
- All responses are streamed for better performance
- Headers are case-insensitive
