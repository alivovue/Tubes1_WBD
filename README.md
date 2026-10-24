# Tubes1_WBD

The website renders HTML with `html/template`, serves its existing styles and
images, and provides the product API. Access control is a backend package;
the website does not use it yet.

## Repository structure

```text
src/
  go.mod                    # Go module and backend dependencies
  main.go                   # Original website entry point
  backend/
    server.go               # Original website routes and product loading
    Products.json           # Product data loaded at startup
    access/                 # Backend services, JSON API and middleware
  database/                 # Platform metadata schema and setup instructions
  frontend/
    templates/              # Go HTML templates and shared navigation/footer
    static/                 # CSS and browser JavaScript
    public/                 # Images
```

## Run locally

Use the Go version declared in `src/go.mod` (Go 1.26+, suggested toolchain
1.26.10), then run from the repository root:

```sh
cd src
go run .
```

Open http://localhost:3000. The original page URLs (`/index.html` through
`/index5.html`) remain available, and `/` opens the home page. This command
requires no PostgreSQL database, master key, or authentication configuration.

Product cards render in the store template. The `/Products` API returns the
product JSON. Images remain available under `/public/`, and styles retain
their existing URLs.

The Go module and entry point are both in `src/`. Node.js and npm are no longer
needed to run the site.

## Access-control backend

`src/backend/access` provides authentication, sessions, group permissions,
platform administration, PostgreSQL connection management, and JSON endpoints.
Initialize an `access.Service` with a PostgreSQL metadata database and
`access.Config`, then mount its `Handler()` when integrating the frontend.

The API has no HTML templates, CSS, login page, or dashboard. The original
website routes remain independent; no access-control routes are mounted there.

## Build

From `src/`:

```sh
go vet ./...
go build -o bin/server .
./bin/server
```

Run the binary from `src/` so it can find `backend/Products.json` and the
`frontend/` templates and assets.
