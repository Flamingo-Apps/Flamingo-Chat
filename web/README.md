# web

Flamingo's web app: React, Vite and TypeScript. It talks only to the gateway. Design and screens are in `docs/ARCHITECTURE.md` §10.

```sh
npm install
npm run dev        # http://localhost:8000
npm run dev:mock   # a stand-in gateway on :8080, so the UI can be used without the Tilt stack
npm run typecheck && npm run lint && npm test && npm run build
```

`dev` talks to `http://localhost:8080` by default, which is where either the Tilt stack's gateway or the mock listens (not both at once). Point it elsewhere with `VITE_GATEWAY_URL` and `VITE_GATEWAY_WS_URL`. Port 8000 is fixed because Google's OAuth client only authorizes that origin.

The mock (`dev/mock-gateway.mjs`) speaks the gateway's HTTP and WebSocket protocol with fake data: guest signup, an instant "Google" sign-in, the once-only gender choice, channels, a bot to match with, and a wandering online count. It does not model the real gateway's rules, so real behaviour is checked against the real stack.
