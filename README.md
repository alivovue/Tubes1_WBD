# Tubes1_WBD

## Repository structure

- `frontend/src/`: HTML pages, stylesheets, and browser JavaScript.
- `frontend/public/`: All images, including product images and `Postman.png`.
- `backend/src/`: Express server code.
- `backend/`: Product data and npm package files; setup files stay outside `src/`.

## Run locally

```sh
cd backend
npm ci
npm start
```

Open http://localhost:3000. The backend serves the frontend and images at
`/public/`, along with the existing `/Products` and `/weather` endpoints.

After installing dependencies, you can also start the server from the repository
root with `node backend/src/index.mjs`.
