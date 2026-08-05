# Relay web prototype

This app is a TanStack Start-compatible frontend prototype beside the existing Go/templ/htmx Relay UI.

## Install

```bash
cd apps/web
npm install
```

## Run dev

```bash
cd apps/web
npm run dev
```

## Build

```bash
cd apps/web
npm run build
```

## Environment

Set the Relay backend URL when needed:

```env
VITE_RELAY_API_BASE_URL=http://localhost:18080
```

The current prototype uses mock data only. Run the existing Go backend on port 18080 during future API integration passes.

## todo

drift detection
