# LivePoll

Create a poll, share the link, and watch the votes arrive live. Everyone viewing a poll sees results move the moment a vote is cast, with no refresh.

**Live app:** `<add your deployed URL here>`

| Layer    | Tech          | What it actually does                                                                 |
| -------- | ------------- | ------------------------------------------------------------------------------------- |
| Frontend | React (Vite)  | Auth screens, poll builder, public vote page, live results over WebSocket             |
| Backend  | Go (Gin)      | Auth (JWT + bcrypt), validation, voting rules, rate limiting, WebSocket hub           |
| Database | MongoDB       | Source of truth: users, polls, and every individual vote                              |
| Realtime | Redis         | Live vote counters, pub/sub fan-out to all viewers, "watching now" presence, rate limits |

```
/frontend   React app (Vite)
/backend    Go service
  config/     env loading + validation
  models/     documents and JSON views
  store/      MongoDB access
  realtime/   Redis broker, WebSocket hub
  handlers/   HTTP endpoints (auth, polls, ws)
  middleware/ JWT auth, rate limit, body limit, headers
Dockerfile, docker-compose.yml
```

## How a vote travels

```
browser --POST /vote--> Gin handler
                         1. validate poll, option, voter id
                         2. INSERT vote into MongoDB   (unique index = one vote per voter)
                         3. Lua script in Redis: HINCRBY the option counter
                         4. PUBLISH full counts on  poll:<id>:events
Redis pub/sub --> every server instance's hub --> every viewer's WebSocket --> bars animate
```

## Run it locally

### Option A: Docker (easiest)

```bash
docker compose up --build
# open http://localhost:8080
```

### Option B: run the pieces yourself

You need Go 1.23+, Node 20+, a MongoDB and a Redis (local or hosted).

```bash
# backend
cd backend
cp .env.example .env        # fill in MONGODB_URI, REDIS_URL, JWT_SECRET (openssl rand -hex 32)
go mod tidy                 # first run only: resolves dependencies, writes go.sum
export $(grep -v '^#' .env | xargs) && go run .

# frontend (second terminal)
cd frontend
npm install
npm run dev                 # http://localhost:5173, proxies /api and /ws to :8080
```

## Key decisions

- **MongoDB is the truth, Redis is the live layer.** A vote is written to MongoDB first, then counted in Redis. If Redis loses its data, counters are rebuilt from MongoDB on the next read, so nothing is lost. If Redis is down, polls still load (from MongoDB) and votes are still stored.
- **One vote per voter is enforced by the database.** A unique index on `(pollId, voterKey)` makes double voting impossible even under concurrent requests. The browser keeps a random ID and the server stores only its SHA-256.
- **Redis does real work in four places:** atomic counters (Lua script), pub/sub fan-out, presence ("N watching now" as a sorted set that self-heals when connections drop), and a shared per-IP rate limiter.
- **Pub/sub makes it scale horizontally.** Each instance subscribes once to `poll:*:events` and forwards to its own WebSocket rooms, so a vote handled by instance A reaches viewers connected to instance B.
- **Clients receive full counts, not increments.** A dropped or repeated message cannot corrupt the display, and the client ignores any message with a lower total than it already shows.
- **Validation lives on the server.** Question and option length, option count, duplicates, control characters, email format, password length (bcrypt's 72-byte limit), voter ID format and poll slug format are all checked before anything touches MongoDB. Bodies are capped at 64 KB.
- **Auth is deliberately simple but real.** Sign up / log in with bcrypt (cost 12) and a 72-hour HS256 JWT. Creating, closing, reopening and deleting polls require a valid token, and owner-only actions are filtered by owner ID in the query itself. Login does a dummy bcrypt compare for unknown emails so timing doesn't reveal which emails exist. Login and signup are rate limited.
- **WebSocket hardening:** origin check against an allow-list, read limit, ping/pong with deadlines, bounded send queue (slow clients are dropped and reconnect), and automatic client reconnect with exponential backoff.
- **Separate folders, one deployable.** Frontend and backend code never mix. For hosting, the Go service can also serve the built React files (`STATIC_DIR`), which gives one URL, no CORS, and same-origin WebSockets.

## Extra features (and why)

- **Watching-now count and connection indicator:** viewers can tell the page is truly live, and when it isn't.
- **Close / reopen / delete a poll:** owners need an end to voting; changes reach open pages instantly.
- **QR code and copy-link:** the realistic way to get a room full of people onto a poll.
- **Highlight flash on the bar that just gained a vote:** makes the live updates noticeable.

## API

| Method | Path                      | Auth   | Notes                                              |
| ------ | ------------------------- | ------ | -------------------------------------------------- |
| POST   | `/api/auth/signup`        | no     | `{name, email, password}`                          |
| POST   | `/api/auth/login`         | no     | `{email, password}`                                |
| GET    | `/api/auth/me`            | yes    |                                                    |
| POST   | `/api/polls`              | yes    | `{question, options[]}`                            |
| GET    | `/api/polls`              | yes    | your polls                                         |
| PATCH  | `/api/polls/:slug`        | owner  | `{closed: bool}`                                   |
| DELETE | `/api/polls/:slug`        | owner  | removes poll and its votes                         |
| GET    | `/api/polls/:slug`        | no     | poll + live counts (+ your vote if `X-Voter-Id`)   |
| POST   | `/api/polls/:slug/vote`   | no     | `{optionId}` + `X-Voter-Id` header                 |
| WS     | `/ws/polls/:slug`         | no     | streams `results`, `viewers`, `status`, `deleted`  |
| GET    | `/healthz`                | no     | checks MongoDB and Redis                           |

## Deploy (free-tier friendly)

1. **MongoDB:** create a free cluster on MongoDB Atlas, add a database user, and allow network access from anywhere (or your host's IPs). Copy the `mongodb+srv://...` connection string.
2. **Redis:** create a free database on Upstash (or Redis Cloud). Copy the `rediss://...` URL (TLS).
3. **App:** create a *Web Service from this GitHub repo* on Render (or Railway/Fly), runtime **Docker**. Set environment variables:

   | Variable          | Value                                                     |
   | ----------------- | --------------------------------------------------------- |
   | `MONGODB_URI`     | your Atlas string                                         |
   | `REDIS_URL`       | your `rediss://` URL                                      |
   | `JWT_SECRET`      | output of `openssl rand -hex 32`                          |
   | `ALLOWED_ORIGINS` | your final app URL, e.g. `https://livepoll.onrender.com`  |

   Health check path: `/healthz`. WebSockets work on these platforms with no extra setup.
4. Open the URL, sign up, create a poll, and open the link in a second browser or phone to confirm live updates.

Free web services may sleep when idle, so open the link shortly before anyone is meant to review it.

## Known limits

- Voter identity is per browser: someone can vote again from a private window or another device. Stopping that fully needs required sign-in for voters, which would change the "anyone with the link can vote" flow. The per-IP rate limit only slows abuse.
- JWTs are kept in `localStorage` for simplicity. An httpOnly cookie would be more robust against XSS, at the cost of CSRF handling.
- If Redis loses its counters while two voters hit a cold cache at the same instant, a count can briefly lag until the next rebuild. MongoDB always holds the correct votes.
