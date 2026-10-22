# Tubes1_WBD

The site runs through one Go server. It renders HTML with `html/template`,
serves the existing styles and images, and provides the product and weather APIs.

## Repository structure

```text
src/
  go.mod                    # Go module setup (standard library only)
  main.go                   # Server entry point
  backend/
    server.go               # Routes, product loading, and weather proxy
    Products.json           # Product data loaded at startup
  frontend/
    templates/              # Go HTML templates and shared navigation/footer
    static/                 # CSS and browser JavaScript
    public/                 # Images
```

## Run locally

Install Go 1.22 or newer, then run from the repository root:

```sh
cd src
go run .
```

Open http://localhost:3000. All six page URLs (`/index.html` through
`/index6.html`) remain available, and `/` opens the home page.

Product cards render in the store template. The weather page still fetches
`/weather` in the browser and displays the existing loading and error messages.
The `/Products` API returns the product JSON, and `/weather` returns Jakarta's
current weather from Open-Meteo. Images remain available under `/public/`,
and styles and browser scripts retain their existing URLs.

The Go module and entry point are both in `src/`. Node.js and npm are no longer
needed to run the site.

## Verify and build

From `src/`:

```sh
go test ./...
go vet ./...
go build -o bin/server .
./bin/server
```

Run the binary from `src/` so it can find `backend/Products.json` and the
`frontend/` templates and assets.
