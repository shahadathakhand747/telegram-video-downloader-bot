# Telegram Video Downloader Bot

A production-grade Telegram bot that downloads videos from various platforms using yt-dlp and streams them directly to Telegram without storing files on the server.

## Features

- **Direct Streaming**: Videos are streamed from the source CDN directly to Telegram
- **Zero Server Storage**: No files are written to the server
- **Multi-Platform Support**: YouTube, Facebook, Twitter, Instagram, TikTok, Reddit, Twitch, Vimeo, Dailymotion
- **Quality Selection**: Users can choose their preferred video quality
- **Production Ready**: Includes health checks, webhook support, and Docker deployment

## Architecture

```
User sends URL → Bot extracts info via yt-dlp → User selects quality →
Telegram downloads from CDN → Video sent to user
```

## Supported Platforms

- YouTube (youtube.com, youtu.be)
- Facebook Videos
- Twitter/X Posts
- Instagram Reels & Posts
- TikTok Videos
- Reddit Videos
- Twitch Clips
- Vimeo Videos
- Dailymotion

## Quick Start

### Prerequisites

- Go 1.21+
- yt-dlp
- ffmpeg (for format extraction)

### Local Development

1. Clone the repository:
```bash
git clone https://github.com/yourusername/telegram-video-downloader-bot.git
cd telegram-video-downloader-bot
```

2. Install dependencies:
```bash
go mod download
```

3. Install yt-dlp:
```bash
pip install yt-dlp
```

4. Set environment variables:
```bash
export TELEGRAM_BOT_TOKEN="your_bot_token_here"
export DEBUG="true"  # Optional: Enable debug logging
```

5. Run the bot:
```bash
go run main.go health.go
# Or use Makefile
make run
```

## Deployment to Render

### Method 1: Docker (Recommended)

1. Create a new Web Service on Render
2. Connect your GitHub repository
3. Configure the service:
   - **Root Directory**: (leave empty)
   - **Build Command**: (leave empty - uses Dockerfile)
   - **Start Command**: `./bot`
4. Add environment variables:
   - `TELEGRAM_BOT_TOKEN`: Your bot token from @BotFather
   - `RENDER_EXTERNAL_URL`: Your Render service URL (e.g., `https://my-bot.onrender.com`)
5. Deploy!

### Method 2: Native Build

1. Create a new Web Service on Render
2. Configure:
   - **Build Command**: `make deps && make build`
   - **Start Command**: `./telegram-video-bot`
3. Add environment variables
4. Deploy!

### Render Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `TELEGRAM_BOT_TOKEN` | Yes | Your Telegram bot token |
| `RENDER_EXTERNAL_URL` | Yes (webhook) | Your Render service URL |
| `PORT` | No | HTTP server port (default: 8080) |
| `DEBUG` | No | Enable debug logging |
| `COOKIES_FILE` | No | Path to cookies file for authenticated downloads |

## Docker

### Build Image

```bash
docker build -t telegram-video-bot .
```

### Run Container

```bash
docker run -d \
  --name video-bot \
  -e TELEGRAM_BOT_TOKEN="your_token" \
  -e RENDER_EXTERNAL_URL="https://your-url.onrender.com" \
  -p 8080:8080 \
  telegram-video-bot
```

### Health Check

```bash
curl http://localhost:8080/health
```

## Usage

1. Start a chat with your bot on Telegram
2. Send `/start` to see the welcome message
3. Send a video URL from any supported platform
4. Select your preferred quality from the inline buttons
5. Wait for the video to be uploaded

## Error Handling

The bot handles various error scenarios:

- **Private videos**: Shows informative message
- **Unsupported formats**: Suggests alternative URLs
- **Timeout**: Allows retry
- **Rate limits**: Implements backoff

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check (returns 200 OK) |
| `/healthz` | GET | Health check alias |
| `/ready` | GET | Kubernetes readiness probe |
| `/` | GET | Service info |

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Submit a pull request

## License

MIT License

## Support

For issues and feature requests, please open a GitHub issue.

## Technical Notes

### Video Streaming

Telegram's `sendVideo` API accepts a URL parameter. The bot passes the direct CDN URL from yt-dlp, and Telegram's servers download the video directly. This approach:

- Eliminates server bandwidth costs
- Reduces latency
- Allows larger files (Telegram's 50MB limit still applies)
- Works with any video format supported by yt-dlp

### Session Management

User sessions are stored in-memory using a thread-safe map. Each session contains:

- Video URL
- Title and metadata
- Available formats
- User's selection

Sessions are cleared after video delivery or timeout.

### Webhook vs Polling

- **Webhook mode** (recommended for production): Lower resource usage, instant updates
- **Polling mode** (for local testing): Simple setup, no HTTPS required

The bot automatically selects the mode based on the presence of `RENDER_EXTERNAL_URL`.
